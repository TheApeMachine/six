package huggingface

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/parquet-go/parquet-go"
	"github.com/theapemachine/six/pkg/store/data/provider"
	"github.com/theapemachine/six/pkg/system/console"
)

/*
BabiQASample holds one bAbI QA sample: Visible (context+question), Answer, and Full (same as Visible).
Used for entity/location extraction and retrieval evaluation.
*/
type BabiQASample struct {
	Visible string
	Answer  string
	Full    string
}

/*
BabiQADataset wraps a HuggingFace Dataset configured for bAbI QA.
Parses the story/answer/type structure into BabiQASamples. Defaults: facebook/babi_qa, en-10k-qa1.
*/
type BabiQADataset struct {
	base *Dataset

	once    sync.Once
	samples []BabiQASample
	err     error
}

/*
NewBabiQA creates a BabiQADataset. Accepts same opts as Dataset (repo, subset, etc.).
Defaults repo to facebook/babi_qa, subset to en-10k-qa1.
*/
func NewBabiQA(opts ...datasetOpts) *BabiQADataset {
	base := New(opts...)
	if base.repo == "" {
		base.repo = "facebook/babi_qa"
	}
	if base.subset == "" {
		base.subset = "en-10k-qa1"
	}

	return &BabiQADataset{base: base}
}

/*
Generate returns a channel that emits RawTokens for each byte of Full across all samples.
Closes when done. Loads and parses the bAbI shard on first use.
*/
func (dataset *BabiQADataset) Generate() chan provider.RawToken {
	out := make(chan provider.RawToken, 4096)

	go func() {
		defer close(out)

		samples, err := dataset.Samples()
		if err != nil {
			console.Error(err, "repo", dataset.base.repo, "subset", dataset.base.subset)
			return
		}

		for sampleID, sample := range samples {
			for idx, b := range []byte(sample.Full) {
				out <- provider.RawToken{
					SampleID: uint32(sampleID),
					Symbol:   b,
					Pos:      uint32(idx),
				}
			}
		}
	}()

	return out
}

/*
Samples loads the bAbI shard (once), parses story/answer/type, and returns all BabiQASamples.
Each sample has Visible (context+question), Answer, Full. Safe to call concurrently.
*/
func (dataset *BabiQADataset) Samples() ([]BabiQASample, error) {
	dataset.once.Do(func() {
		dataset.err = dataset.load()
	})

	if dataset.err != nil {
		return nil, dataset.err
	}

	out := make([]BabiQASample, len(dataset.samples))
	copy(out, dataset.samples)

	return out, nil
}

func (dataset *BabiQADataset) load() error {
	shard, branch, err := dataset.base.discoverShard()
	if err != nil {
		return err
	}

	reader, size, err := dataset.base.downloadShard(shard, branch)
	if err != nil {
		return err
	}

	if strings.HasSuffix(shard, ".parquet") {
		return dataset.loadParquet(reader, size)
	}

	return dataset.loadJSON(reader, size)
}

func (dataset *BabiQADataset) loadParquet(reader io.ReaderAt, size int64) error {
	pFile, err := parquet.OpenFile(reader, size)
	if err != nil {
		return fmt.Errorf("huggingface: open parquet: %w", err)
	}

	textCol := findStoryColumn(pFile.Schema(), "text")
	answerCol := findStoryColumn(pFile.Schema(), "answer")
	typeCol := findStoryColumn(pFile.Schema(), "type")
	if textCol < 0 || answerCol < 0 {
		return fmt.Errorf("huggingface: missing bAbI story columns")
	}

	pReader := parquet.NewReader(pFile)
	defer pReader.Close()

	rows := make([]parquet.Row, 1)
	rowCount := 0

	for {
		n, err := pReader.ReadRows(rows)
		if n == 0 && err != nil {
			break
		}

		row := rows[0]
		texts := parquetStrings(row, textCol)
		answers := parquetStrings(row, answerCol)
		types := parquetInts(row, typeCol)

		dataset.samples = append(dataset.samples, buildBabiQASamples(texts, answers, types)...)
		rowCount++
		if dataset.base.maxSamples > 0 && rowCount >= dataset.base.maxSamples {
			return nil
		}
	}

	return nil
}

func (dataset *BabiQADataset) loadJSON(reader io.ReaderAt, size int64) error {
	dec := json.NewDecoder(io.NewSectionReader(reader, 0, size))

	tok, err := dec.Token()
	if err != nil && err != io.EOF {
		return fmt.Errorf("huggingface json: %w", err)
	}

	isArray := false
	if delim, ok := tok.(json.Delim); ok && delim.String() == "[" {
		isArray = true
	} else if err == nil {
		dec = json.NewDecoder(io.NewSectionReader(reader, 0, size))
	}

	rowCount := 0

	for {
		if isArray && !dec.More() {
			_, _ = dec.Token()
			break
		}

		var row map[string]interface{}
		if err := dec.Decode(&row); err != nil {
			if err != io.EOF {
				console.Error(err, "msg", "bAbI JSON decode failure", "row", rowCount)
			}
			if err == io.EOF {
				break
			}
			continue
		}

		texts, answers, types := jsonStoryFields(row["story"])
		dataset.samples = append(dataset.samples, buildBabiQASamples(texts, answers, types)...)
		rowCount++
		if dataset.base.maxSamples > 0 && rowCount >= dataset.base.maxSamples {
			return nil
		}
	}

	return nil
}

func buildBabiQASamples(texts, answers []string, types []int) []BabiQASample {
	context := make([]string, 0, len(texts))
	samples := make([]BabiQASample, 0)
	answerIdx := 0

	for i, rawText := range texts {
		text := strings.TrimSpace(rawText)
		if text == "" {
			continue
		}

		if isBabiQuestion(i, text, types) {
			answer := ""
			for answerIdx < len(answers) {
				answer = strings.TrimSpace(answers[answerIdx])
				answerIdx++
				if answer != "" {
					break
				}
			}
			if answer == "" {
				continue
			}

			parts := append(append([]string{}, context...), text)
			visible := strings.Join(parts, " ")
			samples = append(samples, BabiQASample{
				Visible: visible,
				Answer:  answer,
				Full:    visible + answer,
			})
			continue
		}

		context = append(context, text)
	}

	return samples
}

func isBabiQuestion(idx int, text string, types []int) bool {
	if idx < len(types) {
		return types[idx] != 0
	}

	return strings.HasSuffix(text, "?")
}

func findStoryColumn(schema *parquet.Schema, leaf string) int {
	for i, col := range schema.Columns() {
		if len(col) == 0 || col[0] != "story" {
			continue
		}
		for _, comp := range col[1:] {
			if comp == leaf {
				return i
			}
		}
	}

	return -1
}

func parquetStrings(row parquet.Row, column int) []string {
	if column < 0 {
		return nil
	}

	var values []string
	for _, v := range row {
		if v.Column() != column || v.IsNull() {
			continue
		}
		text := strings.TrimSpace(string(v.ByteArray()))
		values = append(values, text)
	}

	return values
}

func parquetInts(row parquet.Row, column int) []int {
	if column < 0 {
		return nil
	}

	var values []int
	for _, v := range row {
		if v.Column() != column || v.IsNull() {
			continue
		}
		switch v.Kind() {
		case parquet.Int32:
			values = append(values, int(v.Int32()))
		case parquet.Int64:
			values = append(values, int(v.Int64()))
		}
	}

	return values
}

func jsonStoryFields(raw interface{}) ([]string, []string, []int) {
	story, ok := raw.(map[string]interface{})
	if !ok {
		return nil, nil, nil
	}

	return jsonStrings(story["text"]), jsonStrings(story["answer"]), jsonInts(story["type"])
}

func jsonStrings(raw interface{}) []string {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	values := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			values = append(values, strings.TrimSpace(s))
		}
	}

	return values
}

func jsonInts(raw interface{}) []int {
	items, ok := raw.([]interface{})
	if !ok {
		return nil
	}

	values := make([]int, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case float64:
			values = append(values, int(v))
		case int:
			values = append(values, v)
		}
	}

	return values
}


