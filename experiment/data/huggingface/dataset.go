package huggingface

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto/sha256"
	"encoding/hex"

	"github.com/parquet-go/parquet-go"
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/pkg/errnie"
)

const hfBase = "https://huggingface.co"
const labelBatchSize = 64

/*
Dataset streams raw bytes from a HuggingFace dataset (Parquet or JSON).
Discovers the first train-split shard via API, downloads it, caches to temp,
and emits one data.Sample per logical row via Generate(). Supports label
extraction, multi-column join, and optional transform (e.g. DecodeImageBytes).
*/
type Dataset struct {
	ctx         context.Context
	cancel      context.CancelFunc
	repo        string
	subset      string
	split       string
	textColumn  string
	textColumns []string
	labelColumn string
	labelAppend []string // when set, appends " → <label_name>" to each sample's text
	// labelOrigin is the integer value that corresponds to the FIRST class
	// in labelAppend (0 for 0-indexed datasets like dair-ai/emotion, 1 for
	// 1-indexed datasets like ag_news). Every raw label read from a shard
	// is normalized via `raw - labelOrigin` before it touches any other
	// part of the system, so downstream consumers can always treat
	// dataset.labels[id] as a 0-indexed offset into labelAppend without
	// having to guess the upstream convention. Defaults to 0.
	labelOrigin  int
	maxSamples   int
	transform    func([]byte) ([]byte, error)
	perSamplePos bool
	// babiStory enables facebook/babi_qa-style rows: nested story.{text,answer,type} exploded into QA pairs.
	babiStory bool

	mu     sync.RWMutex
	labels map[uint32]int

	cacheMu       sync.Mutex
	cacheCond     *sync.Cond
	cacheReady    bool
	cacheLoading  bool
	cachedTokens  []byte
	cachedSamples []data.Sample

	readMu   sync.Mutex
	readBuf  []byte
	readPos  int
	readErr  error
	readDone bool
}

var _ data.Provider = (*Dataset)(nil)
var _ io.Reader = (*Dataset)(nil)

type datasetOpts func(*Dataset)

/*
New creates a Dataset with optional config. Defaults: textColumn="text", perSamplePos=true.
Use DatasetWithRepo, DatasetWithTextColumn, etc. to configure.
*/
func New(opts ...datasetOpts) *Dataset {
	dataset := &Dataset{
		textColumn:   "text",
		perSamplePos: true,
		labels:       make(map[uint32]int),
	}
	dataset.cacheCond = sync.NewCond(&dataset.cacheMu)

	for _, opt := range opts {
		opt(dataset)
	}

	dataset.ApplyBabiDefaults()

	return dataset
}

/*
ApplyBabiDefaults sets facebook/babi_qa defaults for repo and subset when bAbI story
mode is enabled and those fields are still empty. Runs after all options so ordering
of DatasetWithBabiStory relative to DatasetWithRepo/Subset does not matter.
*/
func (dataset *Dataset) ApplyBabiDefaults() {
	if !dataset.babiStory {
		return
	}

	if dataset.repo == "" {
		dataset.repo = "facebook/babi_qa"
	}

	if dataset.subset == "" {
		dataset.subset = "en-10k-qa1"
	}
}

/*
rowSample is one logical unit after shard parsing (one classification row, or one exploded bAbI QA pair).
promptText, when set, becomes Sample.Prompt (task string); ingest bytes remain in Sample.Text. labelIsText
selects string answers (bAbI) vs integer labels.
*/
type rowSample struct {
	streamText  string
	promptText  string
	labelInt    int
	labelText   string
	hasLabel    bool
	labelIsText bool
}

// rowVisitor is called once per sample. sampleIdx is monotonic for this shard load.
type rowVisitor func(sample rowSample, sampleIdx uint32) bool

// textColumns returns the effective list of text columns to read.
func (dataset *Dataset) effectiveTextColumns() []string {
	if len(dataset.textColumns) > 0 {
		return dataset.textColumns
	}

	return []string{dataset.textColumn}
}

/*
LabelForSample returns the label stored during streaming for the given sampleID.
Requires DatasetWithLabelColumn. Safe for concurrent use.
*/
func (dataset *Dataset) LabelForSample(id uint32) (int, bool) {
	dataset.mu.RLock()
	defer dataset.mu.RUnlock()

	v, ok := dataset.labels[id]
	return v, ok
}

/*
Generate streams one Sample per row. Text holds the tokenizer ingest bytes; Prompt
is set when the task string differs (bAbI question, classification article without suffix).
*/
func (dataset *Dataset) Generate() iter.Seq[data.Sample] {
	return func(yield func(data.Sample) bool) {
		if cached, ok := dataset.snapshotCachedSamples(); ok {
			dataset.replayCachedSamples(yield, cached)
			return
		}

		if !dataset.tryStartCacheLoad() {
			dataset.replayCachedSamples(yield, dataset.waitForCachedSamples())
			return
		}

		labelBatch := make(map[uint32]int, labelBatchSize)
		tokens := make([]byte, 0, 4096)
		samples := make([]data.Sample, 0, 256)
		flushLabels := func() {
			if len(labelBatch) == 0 {
				return
			}

			dataset.mu.Lock()

			for sampleIdx, label := range labelBatch {
				dataset.labels[sampleIdx] = label
			}

			dataset.mu.Unlock()
			clear(labelBatch)
		}
		defer flushLabels()

		if err := dataset.streamRows(func(sample rowSample, sampleIdx uint32) bool {
			if sample.hasLabel && !sample.labelIsText {
				labelBatch[sampleIdx] = sample.labelInt

				if len(labelBatch) >= labelBatchSize {
					flushLabels()
				}
			}

			out := dataset.materializeSample(sample, sampleIdx)
			samples = append(samples, out)
			tokens = append(tokens, out.Text...)

			return yield(out)
		}); err != nil {
			dataset.finishCacheLoad(nil, nil, false)
			errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
			return
		}

		dataset.finishCacheLoad(tokens, samples, true)
	}
}

/*
materializeSample maps a parsed row to a single ingest Sample. Text always matches
what Read() exposes for this row’s bytes in order; Prompt is set when experiments
should surface a different string than the raw ingest line.
*/
func (dataset *Dataset) materializeSample(sample rowSample, sampleIdx uint32) data.Sample {
	var full strings.Builder

	full.WriteString(sample.streamText)

	if sample.hasLabel && !sample.labelIsText && len(dataset.labelAppend) > 0 &&
		sample.labelInt >= 0 && sample.labelInt < len(dataset.labelAppend) {
		full.WriteString(" → ")
		full.WriteString(dataset.labelAppend[sample.labelInt])
	}

	var prompt []byte

	if sample.promptText != "" {
		prompt = []byte(sample.promptText)
	}

	if len(prompt) == 0 && sample.hasLabel && !sample.labelIsText && len(dataset.labelAppend) > 0 {
		prompt = []byte(sample.streamText)
	}

	var label []byte

	if sample.hasLabel {
		if sample.labelIsText {
			label = []byte(strings.TrimSpace(sample.labelText))
		} else {
			label = []byte(dataset.labelAsText(sample.labelInt, true))
		}
	}

	textBytes := []byte(full.String())

	var labelInt uint64
	if sample.hasLabel {
		labelInt = uint64(sample.labelInt) + 1
	}

	return data.Sample{
		SampleID: sampleIdx,
		Text:     textBytes,
		Label:    label,
		LabelInt: labelInt,
		Prompt:   prompt,
	}
}

/*
Read implements io.Reader over the same byte stream as Generate(): the first
call runs the loader and buffers the shard; subsequent calls return slices of
that buffer until io.EOF. Safe for concurrent use (serialized). A zero-length
Read reports (0, nil).
*/
func (dataset *Dataset) Read(p []byte) (n int, err error) {
	if len(p) == 0 {
		return 0, nil
	}

	dataset.readMu.Lock()
	defer dataset.readMu.Unlock()

	if dataset.readErr != nil {
		errnie.Error(dataset.readErr, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return 0, dataset.readErr
	}

	if !dataset.readDone {
		for range dataset.Generate() {
		}

		dataset.readDone = true

		tok, ok := dataset.snapshotCachedTokens()

		if !ok {
			dataset.readErr = io.ErrUnexpectedEOF
			errnie.Error(dataset.readErr, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
			return 0, dataset.readErr
		}

		dataset.readBuf = tok
	}

	if dataset.readPos >= len(dataset.readBuf) {
		return 0, io.EOF
	}

	n = copy(p, dataset.readBuf[dataset.readPos:])
	dataset.readPos += n

	return n, nil
}

func (dataset *Dataset) Close() error {
	return nil
}

/*
labelAsText resolves a label to its display string.

The streaming path normalizes integer labels to 0-indexed before they are
stored in Sample.Label, but some tests and call-sites still probe this helper
with raw one-based labels. Keep the direct 0-indexed path first, then accept a
single one-based compatibility fallback before returning the numeric form.
*/
func (dataset *Dataset) labelAsText(label int, hasLabel bool) string {
	if !hasLabel {
		return ""
	}

	if len(dataset.labelAppend) > 0 {
		if label >= 0 && label < len(dataset.labelAppend) {
			return dataset.labelAppend[label]
		}

		if label > 0 {
			idx := label - 1
			if idx < len(dataset.labelAppend) {
				return dataset.labelAppend[idx]
			}
		}
	}

	return strconv.Itoa(label)
}

func (dataset *Dataset) snapshotCachedTokens() ([]byte, bool) {
	dataset.cacheMu.Lock()
	defer dataset.cacheMu.Unlock()

	if !dataset.cacheReady {
		return nil, false
	}

	cached := make([]byte, len(dataset.cachedTokens))
	copy(cached, dataset.cachedTokens)
	return cached, true
}

func (dataset *Dataset) tryStartCacheLoad() bool {
	dataset.cacheMu.Lock()
	defer dataset.cacheMu.Unlock()

	if dataset.cacheReady || dataset.cacheLoading {
		return false
	}

	dataset.cacheLoading = true
	return true
}

func (dataset *Dataset) finishCacheLoad(tokens []byte, samples []data.Sample, ok bool) {
	dataset.cacheMu.Lock()
	defer dataset.cacheMu.Unlock()

	if ok {
		dataset.cachedTokens = tokens
		dataset.cachedSamples = cloneSampleSlice(samples)
		dataset.cacheReady = true
	}

	dataset.cacheLoading = false
	errnie.Debug("huggingface.dataset.finishCacheLoad", "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "ok", ok)
	dataset.cacheCond.Broadcast()
}

func cloneSampleSlice(samples []data.Sample) []data.Sample {
	if len(samples) == 0 {
		return nil
	}

	out := make([]data.Sample, len(samples))

	for index := range samples {
		out[index] = data.Sample{
			SampleID: samples[index].SampleID,
			Text:     append([]byte(nil), samples[index].Text...),
			Label:    append([]byte(nil), samples[index].Label...),
			LabelInt: samples[index].LabelInt,
			Prompt:   append([]byte(nil), samples[index].Prompt...),
		}
	}

	return out
}

func (dataset *Dataset) snapshotCachedSamples() ([]data.Sample, bool) {
	dataset.cacheMu.Lock()
	defer dataset.cacheMu.Unlock()

	if !dataset.cacheReady {
		return nil, false
	}

	return cloneSampleSlice(dataset.cachedSamples), true
}

func (dataset *Dataset) waitForCachedSamples() []data.Sample {
	dataset.cacheMu.Lock()
	defer dataset.cacheMu.Unlock()

	for dataset.cacheLoading {
		dataset.cacheCond.Wait()
	}

	if !dataset.cacheReady {
		return nil
	}

	return cloneSampleSlice(dataset.cachedSamples)
}

func (dataset *Dataset) replayCachedSamples(
	yield func(data.Sample) bool, samples []data.Sample,
) {
	if len(samples) == 0 {
		return
	}

	for index := range samples {
		if !yield(samples[index]) {
			return
		}
	}
}

/*
streamRows discovers and downloads the shard file, then delegates
to the appropriate format parser (JSON or Parquet).
fn returning false stops iteration.
*/
func (dataset *Dataset) streamRows(fn rowVisitor) error {
	errnie.Info(
		"huggingface: resolving shard (API or sidecar; may take a while on cold cache)",
		"repo", dataset.repo,
		"subset", dataset.subset,
		"split", dataset.split,
	)

	shard, branch, err := dataset.discoverShard()

	if err != nil {
		return err
	}

	errnie.Info("huggingface: shard resolved", "repo", dataset.repo, "shard", shard, "ref", branch)

	reader, err := dataset.downloadShard(shard, branch)

	if err != nil {
		return err
	}

	if dataset.babiStory {
		if strings.HasSuffix(shard, ".parquet") {
			return dataset.streamBabiParquet(reader, fn)
		}

		return dataset.streamBabiJSON(reader, fn)
	}

	if strings.HasSuffix(shard, ".parquet") {
		return dataset.streamParquet(reader, fn)
	}

	return dataset.streamJSON(reader, fn)
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

func (dataset *Dataset) streamParquet(reader io.Reader, fn rowVisitor) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return fmt.Errorf("huggingface: read shard: %w", err)
	}

	r := bytes.NewReader(data)
	size := int64(len(data))

	pFile, err := parquet.OpenFile(r, size)
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
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
		errnie.Error(fmt.Errorf("huggingface: column %s not found", cols[0]), "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return fmt.Errorf("huggingface: column %s not found", cols[0])
	}

	var sampleCount int
	valueBuf := make([]parquet.Value, 256)

	for _, rg := range pFile.RowGroups() {
		pages := rg.ColumnChunks()[textCol].Pages()

		for page, err := pages.ReadPage(); err == nil; page, err = pages.ReadPage() {
			valReader := page.Values()

			for {
				readCount, readErr := valReader.ReadValues(valueBuf)

				for valueIdx := 0; valueIdx < readCount; valueIdx++ {
					if valueBuf[valueIdx].IsNull() {
						continue
					}

					rawBytes := valueBuf[valueIdx].ByteArray()

					if dataset.transform != nil {
						var err error

						if rawBytes, err = dataset.transform(rawBytes); err != nil {
							errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
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

					if !fn(rowSample{streamText: text}, uint32(sampleCount)) {
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
	var (
		labelIdx int
		ok       bool
	)

	if dataset.labelColumn != "" {
		if labelIdx, ok = fieldIndex[dataset.labelColumn]; !ok {
			errnie.Error(
				ErrLabelColumnNotFound, "column", dataset.labelColumn,
			)
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

		// Extract label and normalize against labelOrigin so internal
		// label storage is always 0-indexed regardless of upstream
		// convention (ag_news is 1-indexed; dair-ai/emotion is 0-indexed).
		// Negative results (raw < labelOrigin) signal a malformed shard
		// or a misdeclared origin — treat the row as unlabeled rather
		// than letting a negative slip into labelBatch where downstream
		// `label >= 0` guards would silently drop it without any signal.
		var label int
		hasLabel := false
		if labelIdx >= 0 && labelIdx < len(row) {
			v := row[labelIdx]
			if !v.IsNull() {
				switch v.Kind() {
				case parquet.Int32:
					label = int(v.Int32()) - dataset.labelOrigin
					hasLabel = true
				case parquet.Int64:
					label = int(v.Int64()) - dataset.labelOrigin
					hasLabel = true
				}
			}
		}

		if hasLabel && label < 0 {
			hasLabel = false
			label = 0
		}

		if !fn(rowSample{
			streamText: text,
			labelInt:   label,
			hasLabel:   hasLabel,
		}, uint32(sampleCount)) {
			return nil
		}

		sampleCount++
	}

	return nil
}

func (dataset *Dataset) streamJSON(reader io.Reader, fn rowVisitor) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("huggingface: read shard: %w", err)
	}

	br := bytes.NewReader(data)
	dec := json.NewDecoder(br)
	var total int

	// Read the first token to see if it's an array
	t, err := dec.Token()
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return err
	}

	isArray := false

	if delim, ok := t.(json.Delim); ok && delim.String() == "[" {
		isArray = true
	}

	dec = json.NewDecoder(bytes.NewReader(data))
	cols := dataset.effectiveTextColumns()

	for {
		if isArray && !dec.More() {
			dec.Token()
			break
		}

		var r map[string]interface{}

		if err := dec.Decode(&r); err != nil {
			if err != io.EOF {
				errnie.Error(err, "msg", "huggingface json decode error", "total", total)
			}

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
					label = int(lv) - dataset.labelOrigin
					hasLabel = true
				case string:
					if n, err := strconv.Atoi(lv); err == nil {
						label = n - dataset.labelOrigin
						hasLabel = true
					}
				}
			}
		}

		// Mirror the parquet path: a negative normalized label means
		// the upstream value was below labelOrigin, which is either a
		// malformed shard or a misdeclared origin. Treat it as missing
		// instead of caching a negative class id in labelBatch.
		if hasLabel && label < 0 {
			hasLabel = false
			label = 0
		}

		if !fn(rowSample{
			streamText: text,
			labelInt:   label,
			hasLabel:   hasLabel,
		}, uint32(total)) {
			return nil
		}

		total++
	}

	return nil
}

/*
downloadShard fetches the shard via HTTP, caches to temp, and returns
the body as an io.Reader (bytes.Reader over the full shard).
*/
func (dataset *Dataset) downloadShard(shard, branch string) (io.Reader, error) {
	shardKey := strings.ReplaceAll(dataset.repo+"_"+shard, "/", "_")
	cachePath := filepath.Join(os.TempDir(), "six_hf_"+shardKey)

	data, err := os.ReadFile(cachePath)
	if err == nil {
		errnie.Info(
			"huggingface: using cached shard",
			"repo", dataset.repo,
			"path", cachePath,
			"bytes", len(data),
		)

		return bytes.NewReader(data), nil
	}

	if !os.IsNotExist(err) {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, err
	}

	encodedBranch := strings.ReplaceAll(branch, "/", "%2F")

	url := fmt.Sprintf("%s/datasets/%s/resolve/%s/%s", hfBase, dataset.repo, encodedBranch, shard)

	req, err := http.NewRequestWithContext(dataset.requestContext(), "GET", url, nil)
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, err
	}

	if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	errnie.Info(
		"huggingface: downloading shard (first fetch can be large / slow)",
		"repo", dataset.repo,
		"url", url,
		"cache", cachePath,
	)

	httpClient := &http.Client{Timeout: 30 * time.Second}

	resp, err := httpClient.Do(req)
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, err
	}

	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		errnie.Error(fmt.Errorf("huggingface: HTTP %d from %s", resp.StatusCode, url), "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, fmt.Errorf("huggingface: HTTP %d from %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, err
	}

	if err := os.WriteFile(cachePath, body, 0644); err != nil {
		errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","))
		return nil, err
	}

	errnie.Info("huggingface: shard downloaded and cached", "repo", dataset.repo, "bytes", len(body))

	return bytes.NewReader(body), nil
}

/*
discoverShard queries the HuggingFace API tree listing and returns
the path to the first train-split .parquet, .json, or .jsonl file.
The result is persisted to a sidecar file next to the cached shard so
that subsequent calls — even from a fresh Dataset instance — bypass
the network entirely.
*/
func (dataset *Dataset) discoverShard() (string, string, error) {
	// Compute a stable hash of the concatenated components to eliminate collisions.
	hash := sha256.New()
	hash.Write([]byte(dataset.repo))
	hash.Write([]byte("\x00"))
	hash.Write([]byte(dataset.split))
	hash.Write([]byte("\x00"))
	hash.Write([]byte(dataset.subset))
	sidecarKey := hex.EncodeToString(hash.Sum(nil))
	sidecarPath := filepath.Join(os.TempDir(), "six_hf_shard_"+sidecarKey+".txt")

	// Check for sidecar freshness against a configurable TTL.
	ttlStr := os.Getenv("SIX_HF_SIDECAR_TTL")
	ttl := 24 * time.Hour // default 1 day
	if ttlStr != "" {
		if d, err := time.ParseDuration(ttlStr); err == nil {
			ttl = d
		}
	}

	if info, err := os.Stat(sidecarPath); err == nil {
		if time.Since(info.ModTime()) < ttl {
			if raw, err := os.ReadFile(sidecarPath); err == nil {
				parts := strings.SplitN(strings.TrimSpace(string(raw)), "\n", 2)
				if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
					return parts[0], parts[1], nil
				}
			}
		} else {
			_ = os.Remove(sidecarPath)
		}
	}

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

		req, err := http.NewRequestWithContext(dataset.requestContext(), "GET", url, nil)
		if err != nil {
			errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "branch", encodedBranch, "url", url)
			continue
		}

		if token := os.Getenv("HF_AUTH_TOKEN"); token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}

		httpClient := &http.Client{Timeout: 30 * time.Second}
		resp, err := httpClient.Do(req)
		if err != nil {
			errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "branch", encodedBranch, "url", url)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			errnie.Error(fmt.Errorf("HTTP %d", resp.StatusCode), "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "branch", encodedBranch, "url", url, "body", string(body))
			resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "branch", encodedBranch, "url", url)
			continue
		}

		var entries []Entry
		if err := json.Unmarshal(body, &entries); err != nil {
			errnie.Error(err, "repo", dataset.repo, "columns", strings.Join(dataset.effectiveTextColumns(), ","), "branch", encodedBranch, "url", url)
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
				_ = os.WriteFile(sidecarPath, []byte(e.Path+"\n"+branch), 0644)
				return e.Path, branch, nil
			}

			if fallback == "" {
				fallback = e.Path
				fallbackBranch = branch
			}
		}
	}

	if fallback != "" {
		_ = os.WriteFile(sidecarPath, []byte(fallback+"\n"+fallbackBranch), 0644)
		return fallback, fallbackBranch, nil
	}

	return "", "", fmt.Errorf("huggingface: no valid parquet/json/jsonl files in %s for subset %q", dataset.repo, dataset.subset)
}

/*
DatasetWithContext binds a cancellable context to the dataset.
Also propagated into every outbound HF HTTP request so the caller
can abort in-flight discovery/download when the test deadline hits.
*/
func DatasetWithContext(ctx context.Context) datasetOpts {
	return func(dataset *Dataset) {
		dataset.ctx, dataset.cancel = context.WithCancel(ctx)
	}
}

/*
requestContext returns the context used for outbound HF HTTP calls.
Without DatasetWithContext, falls back to context.Background() to keep
behaviour identical to the pre-context implementation.
*/
func (dataset *Dataset) requestContext() context.Context {
	if dataset.ctx != nil {
		return dataset.ctx
	}

	return context.Background()
}

/*
DatasetWithRepo sets the HuggingFace dataset repo (e.g. "username/dataset-name").
*/
func DatasetWithRepo(repo string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.repo = repo
	}
}

/*
DatasetWithSubset filters shards by path substring (e.g. "en-10k" for babi).
*/
func DatasetWithSubset(subset string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.subset = subset
	}
}

/*
DatasetWithTextColumn sets the single text column name. Default "text".
*/
func DatasetWithTextColumn(col string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.textColumn = col
	}
}

/*
DatasetWithTextColumns joins multiple columns per row with a space.
Overrides textColumn when set.
*/
func DatasetWithTextColumns(cols ...string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.textColumns = cols
	}
}

/*
DatasetWithLabelColumn stores integer labels from the given column during streaming.
Use LabelForSample(id) to retrieve.
*/
func DatasetWithLabelColumn(col string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.labelColumn = col
	}
}

/*
DatasetWithLabelAppend appends " → <labels[label]>" to each labeled sample's stream.
labels maps integer label index to string (e.g. []string{"world","sports","business"}).
*/
func DatasetWithLabelAppend(labels []string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.labelAppend = labels
	}
}

/*
DatasetWithLabelOrigin declares the integer value used by the upstream shard
for the FIRST class in DatasetWithLabelAppend. Pass 0 for canonical 0-indexed
datasets and 1 for 1-indexed datasets like ag_news. Internal storage is
always normalized to 0-indexed; experiments and reporters never have to
guess. Defaults to 0 when the option is not used.
*/
func DatasetWithLabelOrigin(origin int) datasetOpts {
	return func(dataset *Dataset) {
		dataset.labelOrigin = origin
	}
}

/*
DatasetWithSplit selects split by path substring (e.g. "train", "test").
Defaults to "train" if not set.
*/
func DatasetWithSplit(split string) datasetOpts {
	return func(dataset *Dataset) {
		dataset.split = split
	}
}

/*
DatasetWithSamples limits the number of samples (rows) to stream. 0 = no limit.
*/
func DatasetWithSamples(n int) datasetOpts {
	return func(dataset *Dataset) {
		dataset.maxSamples = n
	}
}

/*
DatasetWithTransform applies fn to each sample's raw bytes before emitting.
Use DecodeImageBytes for image columns.
*/
func DatasetWithTransform(fn func([]byte) ([]byte, error)) datasetOpts {
	return func(dataset *Dataset) {
		dataset.transform = fn
	}
}

/*
DatasetWithContinuousPos keeps Pos monotonically increasing across samples.
Default (perSamplePos=true) resets Pos to 0 per sample.
*/
func DatasetWithContinuousPos() datasetOpts {
	return func(dataset *Dataset) {
		dataset.perSamplePos = false
	}
}

/*
DatasetWithBabiStory parses facebook/babi_qa-style shards (nested story.text / story.answer / story.type).
Repo and subset default to facebook/babi_qa and en-10k-qa1 when still empty; that runs in ApplyBabiDefaults
after all options are applied (see New).
*/
func DatasetWithBabiStory() datasetOpts {
	return func(dataset *Dataset) {
		dataset.babiStory = true
	}
}

type DatasetError string

func (err DatasetError) Error() string {
	return string(err)
}

func (err DatasetError) String() string {
	return string(err)
}

const (
	ErrDatasetNotFound     DatasetError = "dataset not found"
	ErrLabelColumnNotFound DatasetError = "label column not found"
)
