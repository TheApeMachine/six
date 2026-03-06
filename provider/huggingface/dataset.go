package huggingface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/theapemachine/six/console"
	"github.com/theapemachine/six/provider"
)

const hfBase = "https://huggingface.co"

/*
Dataset streams raw bytes from a HuggingFace parquet dataset.
It discovers the first train-split parquet shard, downloads it
via the Fiber/fasthttp client, and emits column values through
a channel as byte-position pairs.
*/
type Dataset struct {
	repo         string
	subset       string
	textColumn   string
	maxSamples   int
	transform    func([]byte) ([]byte, error)
	perSamplePos bool
}

type datasetOpts func(*Dataset)

func New(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		textColumn:   "text",
		perSamplePos: true,
	}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
}

/*
Generate streams the column as (byte, position) pairs.
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

			if dataset.perSamplePos {
				pos = 0
			}

			return true
		}); err != nil {
			console.Error(err, "repo", dataset.repo, "column", dataset.textColumn)
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
	shard, branch, err := dataset.discoverShard()

	if err != nil {
		return err
	}

	reader, size, err := dataset.downloadShard(shard, branch)

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
		// Exact match cases
		if len(col) > 0 && col[0] == name {
			if len(col) == 1 {
				return i
			}

			if len(col) == 2 && col[1] == "bytes" {
				return i
			}

			// If it's a nested structure (like bAbI "story" list)
			for j, comp := range col {
				if comp == "text" && j > 0 {
					return i
				}
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

	sampleCount := 0
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

					rawBytes := valueBuf[i].ByteArray()

					if dataset.transform != nil {
						var err error
						if rawBytes, err = dataset.transform(rawBytes); err != nil {
							continue
						}
					}

					text := string(rawBytes)

					if text == "" {
						continue
					}

					if dataset.maxSamples > 0 && sampleCount >= dataset.maxSamples {
						pages.Close()
						return nil
					}

					if !fn(text) {
						pages.Close()
						return nil
					}

					sampleCount++
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

		if dataset.maxSamples > 0 && total >= dataset.maxSamples {
			return nil
		}

		if !fn(text) {
			return nil
		}

		total++
	}

	return nil
}

/*
downloadShard streams the download via the Fiber client and returns
a bytes.Reader (which implements io.ReaderAt) along with the size.
*/
func (dataset *Dataset) downloadShard(shard, branch string) (io.ReaderAt, int64, error) {

	shardKey := strings.ReplaceAll(dataset.repo+"_"+shard, "/", "_")
	cachePath := filepath.Join(os.TempDir(), "six_hf_"+shardKey)

	if data, err := os.ReadFile(cachePath); err == nil {
		r := bytes.NewReader(data)
		return r, r.Size(), nil
	}

	encodedBranch := strings.ReplaceAll(branch, "/", "%2F")
	url := fmt.Sprintf("%s/datasets/%s/resolve/%s/%s", hfBase, dataset.repo, encodedBranch, shard)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("huggingface req: %w", err)
	}

	if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	httpClient := &http.Client{}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("huggingface: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, 0, fmt.Errorf("huggingface: HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("huggingface read: %w", err)
	}

	_ = os.WriteFile(cachePath, body, 0644)

	r := bytes.NewReader(body)

	return r, r.Size(), nil
}

/*
discoverShard queries the HuggingFace API tree listing and returns
the path to the first train-split .parquet, .json, or .jsonl file,
or any valid fallback.
*/
func (dataset *Dataset) discoverShard() (string, string, error) {
	branches := []string{"main", "refs/convert/parquet"}

	var fallback string
	var fallbackBranch string

	type Entry struct {
		Type string `json:"type"`
		Path string `json:"path"`
	}

	for _, branch := range branches {
		encodedBranch := strings.ReplaceAll(branch, "/", "%2F")
		url := fmt.Sprintf("%s/api/datasets/%s/tree/%s?recursive=true", hfBase, dataset.repo, encodedBranch)

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}

		if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		httpClient := &http.Client{}
		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		var entries []Entry
		if err := json.Unmarshal(body, &entries); err != nil {
			continue
		}

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

			if dataset.subset != "" && !strings.Contains(e.Path, dataset.subset) {
				continue
			}

			if strings.Contains(e.Path, "train") {
				return e.Path, branch, nil
			}

			if fallback == "" {
				fallback = e.Path
				fallbackBranch = branch
			}
		}
	}

	if fallback != "" {
		return fallback, fallbackBranch, nil
	}

	return "", "", fmt.Errorf("huggingface: no valid parquet/json/jsonl files in %s for subset %q", dataset.repo, dataset.subset)
}

func DatasetWithRepo(repo string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.repo = repo
	}
}

func DatasetWithSubset(subset string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.subset = subset
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

func DatasetWithTransform(fn func([]byte) ([]byte, error)) datasetOpts {
	return func(dataset *Dataset) {
		dataset.transform = fn
	}
}

func DatasetWithPerSamplePos() datasetOpts {
	return func(dataset *Dataset) {
		dataset.perSamplePos = true
	}
}
