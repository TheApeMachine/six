package huggingface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/gofiber/fiber/v3/client"
	"github.com/segmentio/parquet-go"
)

type Sample struct {
	buf []byte
	seqIdx int
}

/*
Dataset is a streaming adapter for huggingface datasets.
*/
type Dataset struct {
	BaseURL    string
	DatasetID  string
	ShardFile  string
	TextColumn string
	MaxTokens  int

	client     *client.Client
	onProgress func(int64, int64)
}

func New() *Dataset {
	return &Dataset{
		client: client.New(),
	}
}

func (dataset *Dataset) Generate() chan Sample  {
	return nil
}

type hfTreeEntry struct {
	Type string `json:"type"`
	Path string `json:"path"`
	Size int64  `json:"size"`
}

func (dataset *Dataset) apiBase() string {
	if dataset.BaseURL != "" {
		return strings.TrimSuffix(dataset.BaseURL, "/")
	}
	return "https://huggingface.co"
}

func (dataset *Dataset) getConfig() client.Config {
	cfg := client.Config{}

	if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
		cfg.Header = map[string]string{
			"Authorization": "Bearer " + token,
		}
	}

	return cfg
}

/*
discoverParquetFile queries the HuggingFace API to find parquet files
in the dataset repo, preferring train-split shards.
*/
func (dataset *Dataset) discoverParquetFile() (string, error) {
	apiURL := fmt.Sprintf("%s/api/datasets/%s/tree/main?recursive=true", dataset.apiBase(), dataset.DatasetID)
	
	resp, err := dataset.client.Get(apiURL, dataset.getConfig())
	
	if err != nil {
		return "", HuggingFaceDatasetError("build request")
	}

	if resp.StatusCode() != 200 {
		return "", fmt.Errorf("list files for %s: HTTP %d", dataset.DatasetID, resp.StatusCode())
	}

	var entries []hfTreeEntry
	
	if err := json.Unmarshal(resp.Body(), &entries); err != nil {
		return "", HuggingFaceDatasetError("parse file listing")
	}

	var parquetFiles []string
	
	for _, e := range entries {
		if e.Type == "file" && strings.HasSuffix(e.Path, ".parquet") {
			parquetFiles = append(parquetFiles, e.Path)
		}
	}

	if len(parquetFiles) == 0 {
		return "", HuggingFaceDatasetError("no parquet files found")
	}

	for _, f := range parquetFiles {
		if strings.Contains(f, "train") {
			return f, nil
		}
	}

	return parquetFiles[0], nil
}

/*
openParquet downloads the parquet shard into memory and opens it.
*/
func (dataset *Dataset) openParquet() (*parquet.File, error) {
	if dataset.ShardFile == "" {
		shard, err := dataset.discoverParquetFile()
		if err != nil {
			return nil, err
		}
		dataset.ShardFile = shard
	}

	fileURL := fmt.Sprintf("%s/datasets/%s/resolve/main/%s", dataset.apiBase(), dataset.DatasetID, dataset.ShardFile)
	
	resp, err := dataset.client.Get(fileURL, dataset.getConfig())
	
	if err != nil {
		return nil, HuggingFaceDatasetError("download")
	}

	if resp.StatusCode() != 200 {
		return nil, HuggingFaceDatasetError("download")
	}

	// Trigger progress if available (mimicking the previous file loading experience)
	if dataset.onProgress != nil {
		size := int64(len(resp.Body()))
		dataset.onProgress(size, size)
	}

	reader := bytes.NewReader(resp.Body())
	pFile, err := parquet.OpenFile(reader, reader.Size())
	
	if err != nil {
		return nil, HuggingFaceDatasetError("open parquet")
	}

	return pFile, nil
}

func findColumn(schema *parquet.Schema, name string) int {
	for i, col := range schema.Columns() {
		if len(col) == 1 && col[0] == name {
			return i
		}
	}
	return -1
}

func columnNames(schema *parquet.Schema) []string {
	seen := make(map[string]bool)
	var names []string
	for _, col := range schema.Columns() {
		if top := col[0]; !seen[top] {
			seen[top] = true
			names = append(names, top)
		}
	}
	return names
}

/*
extractValue retrieves the typed value from a parquet Value based on the expected generic type T. 
*/
func extractValue[T string | int](val parquet.Value) T {
	var zero T
	switch any(zero).(type) {
	case string:
		switch val.Kind() {
		case parquet.ByteArray, parquet.FixedLenByteArray:
			return any(string(val.ByteArray())).(T)
		default:
			return any(val.String()).(T)
		}
	case int:
		switch val.Kind() {
		case parquet.Int32:
			return any(int(val.Int32())).(T)
		case parquet.Int64:
			return any(int(val.Int64())).(T)
		default:
			return any(0).(T)
		}
	}
	return zero
}

/*
readColumn reads all values of type T from a single column in a row group using generics.
*/
func readColumn[T string | int](rg parquet.RowGroup, colIdx int) []T {
	pages := rg.ColumnChunks()[colIdx].Pages()
	defer pages.Close()

	var result []T
	valueBuf := make([]parquet.Value, 256)
	
	for page, err := pages.ReadPage(); err == nil; page, err = pages.ReadPage() {
		valReader := page.Values()
		for n, readErr := valReader.ReadValues(valueBuf); n > 0 || readErr == nil; n, readErr = valReader.ReadValues(valueBuf) {
			for i := 0; i < n; i++ {
				var val T
				if !valueBuf[i].IsNull() {
					val = extractValue[T](valueBuf[i])
				} else if any(val) == int(0) {
					// Fallback for nullable ints
					val = any(math.MinInt32).(T)
				}
				result = append(result, val)
			}
			if readErr != nil {
				break
			}
		}
	}
	return result
}

/*
streamColumn streams text dynamically with significantly reduced nesting and cleaner iteration.
*/
func (dataset *Dataset) streamColumn(fn func(text string) bool) error {
	pFile, err := dataset.openParquet()
	if err != nil {
		return err
	}

	textCol := findColumn(pFile.Schema(), dataset.TextColumn)

	if textCol < 0 {
		return HuggingFaceDatasetError("column not found")
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
					
					if dataset.MaxTokens > 0 && totalBytes+len(text) > dataset.MaxTokens {
						remaining := dataset.MaxTokens - totalBytes

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

type HuggingFaceDatasetError string

const (
	ErrNoParquetFiles = HuggingFaceDatasetError("no parquet files found")
	ErrColumnNotFound = HuggingFaceDatasetError("column not found")
	ErrBuildRequest = HuggingFaceDatasetError("build request")
	ErrParseFileListing = HuggingFaceDatasetError("parse file listing")
	ErrDownload = HuggingFaceDatasetError("download")
)

func (err HuggingFaceDatasetError) Error() string {
	return string(err)
}