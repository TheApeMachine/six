package huggingface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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
	split        string
	textColumn   string
	textColumns  []string
	labelColumn  string
	labelAppend  []string // when set, appends " → <label_name>" to each sample's text
	maxSamples   int
	transform    func([]byte) ([]byte, error)
	perSamplePos bool

	mu     sync.RWMutex
	labels map[uint32]int
}

type datasetOpts func(*Dataset)

func New(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		textColumn:   "text",
		perSamplePos: true,
		labels:       make(map[uint32]int),
	}

	for _, opt := range opts {
		opt(dataset)
	}

	return dataset
}

// rowVisitor is called once per sample with the joined text, optional label, and sample index.
type rowVisitor func(text string, label int, hasLabel bool, sampleIdx uint32) bool

// textColumns returns the effective list of text columns to read.
func (dataset *Dataset) effectiveTextColumns() []string {
	if len(dataset.textColumns) > 0 {
		return dataset.textColumns
	}
	return []string{dataset.textColumn}
}

// LabelForSample returns the label stored during streaming for the given sampleID.
func (dataset *Dataset) LabelForSample(id uint32) (int, bool) {
	dataset.mu.RLock()
	defer dataset.mu.RUnlock()
	v, ok := dataset.labels[id]
	return v, ok
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

		if err := dataset.streamRows(func(text string, label int, hasLabel bool, sampleIdx uint32) bool {
			if hasLabel {
				dataset.mu.Lock()
				dataset.labels[sampleIdx] = label
				dataset.mu.Unlock()
			}

			for _, b := range []byte(text) {
				out <- provider.RawToken{SampleID: sampleIdx, Symbol: b, Pos: pos}
				pos++
			}

			// When labelAppend is configured, append " → <label_name>" to the
			// sample's byte stream so the manifold stores article+label as a
			// single continuous sequence (classification-as-generation).
			if hasLabel && len(dataset.labelAppend) > 0 && label >= 0 && label < len(dataset.labelAppend) {
				suffix := " " + dataset.labelAppend[label]
				for _, b := range []byte(suffix) {
					out <- provider.RawToken{SampleID: sampleIdx, Symbol: b, Pos: pos}
					pos++
				}
			}

			if dataset.perSamplePos {
				pos = 0
			}

			return true
		}); err != nil {
			console.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		}
	}()

	return out
}

/*
streamRows discovers and downloads the shard file, then delegates
to the appropriate format parser (JSON or Parquet).
fn returning false stops iteration.
*/
func (dataset *Dataset) streamRows(fn rowVisitor) error {
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

func (dataset *Dataset) streamParquet(reader io.ReaderAt, size int64, fn rowVisitor) error {
	pFile, err := parquet.OpenFile(reader, size)
	if err != nil {
		return fmt.Errorf("huggingface: open parquet: %w", err)
	}

	cols := dataset.effectiveTextColumns()

	// Multi-column path: use row-level reader to join columns.
	if len(cols) > 1 || dataset.labelColumn != "" {
		return dataset.streamParquetRows(pFile, cols, fn)
	}

	// Single-column fast path: use column-level page iteration.
	textCol := findColumn(pFile.Schema(), cols[0])
	if textCol < 0 {
		return fmt.Errorf("huggingface: column %s not found", cols[0])
	}

	var sampleCount int
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

					if !fn(text, 0, false, uint32(sampleCount)) {
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

// streamParquetRows reads full rows when multi-column join or label extraction is needed.
func (dataset *Dataset) streamParquetRows(pFile *parquet.File, textCols []string, fn rowVisitor) error {
	pReader := parquet.NewReader(pFile)
	defer pReader.Close()

	// Build column name → field index mapping from the schema.
	type colInfo struct {
		name string
		idx  int
	}

	fields := pReader.Schema().Fields()
	fieldIndex := make(map[string]int, len(fields))
	for i, f := range fields {
		fieldIndex[f.Name()] = i
	}

	// Resolve text column indices.
	var textIndices []colInfo
	for _, name := range textCols {
		if idx, ok := fieldIndex[name]; ok {
			textIndices = append(textIndices, colInfo{name, idx})
		} else {
			return fmt.Errorf("huggingface: text column %q not found", name)
		}
	}

	// Resolve optional label column index.
	labelIdx := -1
	if dataset.labelColumn != "" {
		if idx, ok := fieldIndex[dataset.labelColumn]; ok {
			labelIdx = idx
		} else {
			console.Warn(fmt.Sprintf("label column %q not found, continuing without labels",
				dataset.labelColumn))
		}
	}

	rows := make([]parquet.Row, 1)
	var sampleCount int

	for {
		n, err := pReader.ReadRows(rows)
		if n == 0 && err != nil {
			break
		}

		row := rows[0]

		if dataset.maxSamples > 0 && sampleCount >= dataset.maxSamples {
			return nil
		}

		// Join text columns with a space.
		var parts []string
		for _, ci := range textIndices {
			if ci.idx >= len(row) {
				continue
			}
			v := row[ci.idx]
			if v.IsNull() {
				continue
			}
			s := string(v.ByteArray())
			if s != "" {
				parts = append(parts, s)
			}
		}

		text := strings.Join(parts, " ")
		if text == "" {
			continue
		}

		if dataset.transform != nil {
			transformed, err := dataset.transform([]byte(text))
			if err != nil {
				continue
			}
			text = string(transformed)
		}

		// Extract label.
		var label int
		hasLabel := false
		if labelIdx >= 0 && labelIdx < len(row) {
			v := row[labelIdx]
			if !v.IsNull() {
				label = int(v.Int64())
				hasLabel = true
			}
		}

		if !fn(text, label, hasLabel, uint32(sampleCount)) {
			return nil
		}

		sampleCount++
	}

	return nil
}

func (dataset *Dataset) streamJSON(reader io.ReaderAt, size int64, fn rowVisitor) error {
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
		dec = json.NewDecoder(io.NewSectionReader(reader, 0, size))
	}

	cols := dataset.effectiveTextColumns()

	for {
		if isArray && !dec.More() {
			dec.Token()
			break
		}

		var r map[string]interface{}
		if err := dec.Decode(&r); err != nil {
			if err == io.EOF {
				break
			}
			continue
		}

		// Join text columns.
		var parts []string
		for _, col := range cols {
			if v, ok := r[col]; ok {
				if s, ok := v.(string); ok && s != "" {
					parts = append(parts, s)
				}
			}
		}

		text := strings.Join(parts, " ")
		if text == "" {
			continue
		}

		if dataset.maxSamples > 0 && total >= dataset.maxSamples {
			return nil
		}

		// Extract optional label.
		var label int
		hasLabel := false
		if dataset.labelColumn != "" {
			if v, ok := r[dataset.labelColumn]; ok {
				switch lv := v.(type) {
				case float64:
					label = int(lv)
					hasLabel = true
				case string:
					if n, err := strconv.Atoi(lv); err == nil {
						label = n
						hasLabel = true
					}
				}
			}
		}

		if !fn(text, label, hasLabel, uint32(total)) {
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

			targetSplit := dataset.split
			if targetSplit == "" {
				targetSplit = "train"
			}
			if strings.Contains(e.Path, targetSplit) {
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

// DatasetWithTextColumns joins multiple columns per row with a space separator.
func DatasetWithTextColumns(cols ...string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.textColumns = cols
	}
}

// DatasetWithLabelColumn stores integer labels from the given column during streaming.
// Use LabelForSample(id) to retrieve them.
func DatasetWithLabelColumn(col string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.labelColumn = col
	}
}

// DatasetWithLabelAppend enables classification-as-generation by appending the
// label name to each sample's text stream. The labels slice maps integer label
// indices to human-readable names (e.g. []string{"world","sports","business","sci_tech"}).
func DatasetWithLabelAppend(labels []string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.labelAppend = labels
	}
}

// DatasetWithSplit selects which split to load (e.g. "train", "test").
// Defaults to "train" if not set.
func DatasetWithSplit(split string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.split = split
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

func DatasetWithContinuousPos() datasetOpts {
	return func(dataset *Dataset) {
		dataset.perSamplePos = false
	}
}
