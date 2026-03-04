package huggingface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gofiber/fiber/v3/client"
	"github.com/parquet-go/parquet-go"
	"github.com/theapemachine/six/provider"
	"github.com/valyala/fasthttp"
)

const hfBase = "https://huggingface.co"

/*
Dataset streams raw bytes from a HuggingFace parquet dataset.
It discovers the first train-split parquet shard, downloads it
via the Fiber/fasthttp client, and emits column values through
a channel as byte-position pairs.
*/
type Dataset struct {
	repo       string
	textColumn string
	maxSamples int
}

type datasetOpts func(*Dataset)

func New(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		textColumn: "text",
	}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
}

/*
Generate streams the text column as (byte, position) pairs.
The returned channel closes when all data has been emitted.
*/
func (dataset *Dataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken)

	go func() {
		defer close(out)

		var pos uint32
		var sampleID uint32

		if err := dataset.streamRows(func(text string) bool {
			for _, b := range []byte(text) {
				out <- provider.RawToken{SampleID: sampleID, Symbol: b, Pos: pos}
				pos++
			}
			sampleID++

			return true
		}); err != nil {
			fmt.Printf("Dataset error: %v\n", err)
		}
	}()

	return out
}

/*
streamRows discovers and downloads the shard file, then delegates
to the appropriate format parser (JSON or Parquet).
fn returning false stops iteration.
*/
func (dataset *Dataset) streamRows(fn func(string) bool) error {
	shard, err := dataset.discoverShard()
	if err != nil {
		return err
	}

	reader, size, err := dataset.downloadShard(shard)
	if err != nil {
		return err
	}

	if strings.HasSuffix(shard, ".parquet") {
		return dataset.streamParquet(reader, size, fn)
	}

	return dataset.streamJSON(reader, size, fn)
}

func findColumn(schema *parquet.Schema, name string) int {
	for i, col := range schema.Columns() {
		if len(col) > 0 && col[0] == name {
			if len(col) == 1 {
				return i
			}
			if len(col) == 2 && col[1] == "bytes" {
				return i
			}
		}
	}
	return -1
}

func (dataset *Dataset) streamParquet(reader io.ReaderAt, size int64, fn func(string) bool) error {
	pFile, err := parquet.OpenFile(reader, size)
	if err != nil {
		return fmt.Errorf("huggingface: open parquet: %w", err)
	}

	textCol := findColumn(pFile.Schema(), dataset.textColumn)
	if textCol < 0 {
		return fmt.Errorf("huggingface: column %s not found", dataset.textColumn)
	}

	totalBytes := 0
	valueBuf := make([]parquet.Value, 256)

	for _, rg := range pFile.RowGroups() {
		pages := rg.ColumnChunks()[textCol].Pages()

		for page, err := pages.ReadPage(); err == nil; page, err = pages.ReadPage() {
			valReader := page.Values()

			for {
				n, readErr := valReader.ReadValues(valueBuf)

				for i := range n {
					if valueBuf[i].IsNull() {
						continue
					}

					text := string(valueBuf[i].ByteArray())

					if text == "" {
						continue
					}

					if dataset.maxSamples > 0 && totalBytes+len(text) > dataset.maxSamples {
						remaining := dataset.maxSamples - totalBytes

						if remaining > 0 {
							fn(text[:remaining])
						}

						pages.Close()
						return nil
					}

					if !fn(text) {
						pages.Close()
						return nil
					}

					totalBytes += len(text)
				}

				if readErr != nil {
					break
				}
			}
		}

		pages.Close()
	}

	return nil
}

func (dataset *Dataset) streamJSON(reader io.ReaderAt, size int64, fn func(string) bool) error {
	dec := json.NewDecoder(io.NewSectionReader(reader, 0, size))
	var total int

	// Read the first token to see if it's an array
	t, err := dec.Token()
	if err != nil && err != io.EOF {
		return fmt.Errorf("huggingface json: %w", err)
	}

	isArray := false
	if delim, ok := t.(json.Delim); ok && delim.String() == "[" {
		isArray = true
	} else if err == nil {
		// Not an array, so if it's a map we must back up, but we can't un-read from dec.
		// A better approach for JSONL is to decode continuously.
		// Since we already consumed the first token, let's just make a new decoder
		// if it's not an array, to read it cleanly from the start.
		dec = json.NewDecoder(io.NewSectionReader(reader, 0, size))
	}

	for {
		if isArray && !dec.More() {
			// Read the closing bracket
			dec.Token()
			break
		}

		var r map[string]interface{}
		if err := dec.Decode(&r); err != nil {
			if err == io.EOF {
				break
			}
			// Skip malformed entries
			continue
		}

		v, ok := r[dataset.textColumn]
		if !ok {
			continue
		}

		text, ok := v.(string)
		if !ok || text == "" {
			continue
		}

		if dataset.maxSamples > 0 && total+len(text) > dataset.maxSamples {
			if rem := dataset.maxSamples - total; rem > 0 {
				fn(text[:rem])
			}
			return nil
		}

		if !fn(text) {
			return nil
		}

		total += len(text)
	}

	return nil
}

/*
downloadShard streams the download via the Fiber client and returns
a bytes.Reader (which implements io.ReaderAt) along with the size.
*/
func (dataset *Dataset) downloadShard(shard string) (io.ReaderAt, int64, error) {

	shardKey := strings.ReplaceAll(dataset.repo+"_"+shard, "/", "_")
	cachePath := filepath.Join(os.TempDir(), "six_hf_"+shardKey)
	
	if data, err := os.ReadFile(cachePath); err == nil {
		r := bytes.NewReader(data)
		return r, r.Size(), nil
	}

	url := fmt.Sprintf("%s/datasets/%s/resolve/main/%s", hfBase, dataset.repo, shard)
	resp, err := dataset.request(url)

	if err != nil {
		return nil, 0, err
	}
	defer fasthttp.ReleaseResponse(resp.RawResponse)

	body := resp.RawResponse.Body()
	bodyCopy := make([]byte, len(body))
	copy(bodyCopy, body)
	
	_ = os.WriteFile(cachePath, bodyCopy, 0644)
	
	r := bytes.NewReader(bodyCopy)

	return r, r.Size(), nil
}

/*
discoverShard queries the HuggingFace API tree listing and returns
the path to the first train-split .parquet, .json, or .jsonl file,
or any valid fallback.
*/
func (dataset *Dataset) discoverShard() (string, error) {
	url := fmt.Sprintf("%s/api/datasets/%s/tree/main?recursive=true", hfBase, dataset.repo)
	resp, err := dataset.request(url)

	if err != nil {
		return "", err
	}
	defer fasthttp.ReleaseResponse(resp.RawResponse)

	var entries []struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}

	if err := json.Unmarshal(resp.Body(), &entries); err != nil {
		return "", fmt.Errorf("huggingface: parse listing: %w", err)
	}

	var fallback string

	for _, e := range entries {
		if e.Type != "file" {
			continue
		}

		isSupported := strings.HasSuffix(e.Path, ".parquet") ||
			strings.HasSuffix(e.Path, ".json") ||
			strings.HasSuffix(e.Path, ".jsonl")
		if !isSupported {
			continue
		}

		if strings.Contains(e.Path, "train") {
			return e.Path, nil
		}

		if fallback == "" {
			fallback = e.Path
		}
	}

	if fallback == "" {
		return "", fmt.Errorf("huggingface: no valid parquet/json/jsonl files in %s", dataset.repo)
	}

	return fallback, nil
}

/*
request builds and executes a GET via the Fiber client's R() builder.
*/
func (dataset *Dataset) request(url string) (*client.Response, error) {
	req := client.New().R()
	defer fasthttp.ReleaseRequest(req.RawRequest)

	if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
		req.RawRequest.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := req.Get(url)

	if err != nil {
		return nil, fmt.Errorf("huggingface: %w", err)
	}

	code := resp.StatusCode()
	if code == 301 || code == 302 || code == 307 || code == 308 {
		loc := string(resp.RawResponse.Header.Peek("Location"))
		if loc != "" {
			if !strings.HasPrefix(loc, "http") {
				if strings.HasPrefix(loc, "/") {
					loc = hfBase + loc
				} else {
					loc = hfBase + "/" + loc
				}
			}
			fasthttp.ReleaseResponse(resp.RawResponse)
			return dataset.request(loc)
		}
	}

	if code != 200 {
		fasthttp.ReleaseResponse(resp.RawResponse)
		return nil, fmt.Errorf("huggingface: HTTP %d from %s", code, url)
	}

	return resp, nil
}

func DatasetWithRepo(repo string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.repo = repo
	}
}

func DatasetWithTextColumn(col string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.textColumn = col
	}
}

func DatasetWithSamples(n int) datasetOpts {
	return func(dataset *Dataset) {
		dataset.maxSamples = n
	}
}
