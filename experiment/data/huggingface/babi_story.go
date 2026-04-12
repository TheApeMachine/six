package huggingface

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/parquet-go/parquet-go"
	"github.com/theapemachine/six/pkg/errnie"
)

/*
streamBabiParquet reads facebook/babi_qa-style Parquet: each row holds story.{text,answer,type} lists.
Rows are expanded into QA pairs; maxSamples limits top-level Parquet rows (not individual QAs).
*/
func (dataset *Dataset) streamBabiParquet(reader io.Reader, fn rowVisitor) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("huggingface: read shard: %w", err)
	}

	r := bytes.NewReader(data)
	pFile, err := parquet.OpenFile(r, int64(len(data)))
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
	rowsRead := 0
	var sampleIdx uint32

	for {
		n, err := pReader.ReadRows(rows)
		if n == 0 && err != nil {
			break
		}

		if n == 0 {
			break
		}

		if dataset.maxSamples > 0 && rowsRead >= dataset.maxSamples {
			return nil
		}

		row := rows[0]
		texts := parquetStrings(row, textCol)
		answers := parquetStrings(row, answerCol)
		types := parquetInts(row, typeCol)

		for _, qa := range buildBabiQASamples(texts, answers, types) {
			if !fn(babiQAtoRowSample(qa), sampleIdx) {
				return nil
			}

			sampleIdx++
		}

		rowsRead++
	}

	return nil
}

/*
streamBabiJSON reads the same structure from JSON / JSONL shards.
*/
func (dataset *Dataset) streamBabiJSON(reader io.Reader, fn rowVisitor) error {
	data, err := io.ReadAll(reader)
	if err != nil {
		return fmt.Errorf("huggingface: read shard: %w", err)
	}

	br := bytes.NewReader(data)
	dec := json.NewDecoder(br)

	tok, err := dec.Token()
	if err != nil && err != io.EOF {
		return fmt.Errorf("huggingface json: %w", err)
	}

	isArray := false
	if delim, ok := tok.(json.Delim); ok && delim.String() == "[" {
		isArray = true
	} else if err == nil {
		dec = json.NewDecoder(bytes.NewReader(data))
	}

	rowsRead := 0
	var sampleIdx uint32

	for {
		if isArray && !dec.More() {
			_, _ = dec.Token()
			break
		}

		var row map[string]interface{}
		if err := dec.Decode(&row); err != nil {
			if err != io.EOF {
				errnie.Error(err, "msg", "bAbI JSON decode failure", "row", rowsRead)
			}

			if err == io.EOF {
				break
			}

			continue
		}

		if dataset.maxSamples > 0 && rowsRead >= dataset.maxSamples {
			return nil
		}

		texts, answers, types := jsonStoryFields(row["story"])
		for _, qa := range buildBabiQASamples(texts, answers, types) {
			if !fn(babiQAtoRowSample(qa), sampleIdx) {
				return nil
			}

			sampleIdx++
		}

		rowsRead++
	}

	return nil
}

func babiQAtoRowSample(qa babiQASample) rowSample {
	return rowSample{
		streamText:  qa.Full,
		promptText:  extractBabiQuestion(qa.Visible),
		labelText:   qa.Answer,
		hasLabel:    qa.Answer != "",
		labelIsText: true,
	}
}

/*
babiQASample is one expanded QA pair from a bAbI story row.
*/
type babiQASample struct {
	Visible string
	Answer  string
	Full    string
}

/*
buildBabiQASamples converts raw story text/answer/type arrays into QA samples.
Questions (identified by type or trailing "?") get paired with the next answer;
context is accumulated from preceding non-question lines.
*/
func buildBabiQASamples(texts, answers []string, types []int) []babiQASample {
	context := make([]string, 0, len(texts))
	samples := make([]babiQASample, 0)
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
			samples = append(samples, babiQASample{
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

// extractBabiQuestion returns the last sentence ending in "?" from visible,
// or the full visible text if no question sentence is found.
func extractBabiQuestion(visible string) string {
	sentences := strings.Split(visible, ".")
	for i := len(sentences) - 1; i >= 0; i-- {
		s := strings.TrimSpace(sentences[i])
		if strings.HasSuffix(s, "?") {
			return s
		}
	}

	return visible
}

func isBabiQuestion(idx int, text string, types []int) bool {
	if idx < len(types) {
		return types[idx] != 0
	}

	return strings.HasSuffix(text, "?")
}

/*
findStoryColumn returns the Parquet column index for the story.{leaf} field.
*/
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

func jsonStoryFields(raw any) ([]string, []string, []int) {
	story, ok := raw.(map[string]any)
	if !ok {
		return nil, nil, nil
	}

	return jsonStrings(story["text"]), jsonStrings(story["answer"]), jsonInts(story["type"])
}

func jsonStrings(raw any) []string {
	items, ok := raw.([]any)
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

func jsonInts(raw any) []int {
	items, ok := raw.([]any)
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
