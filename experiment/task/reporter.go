package task

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	tools "github.com/theapemachine/six/experiment"
	"github.com/theapemachine/six/experiment/projector"
)

// sectionRegistry tracks all experiment section .tex files written in this
// process so that WriteExperimentsIndex can emit a unified include file.
var (
	sectionMu       sync.Mutex
	sectionRegistry []sectionEntry
)

type sectionEntry struct {
	section  string // e.g. "imagegen"
	fileName string // e.g. "reconstruction_section.tex"
}

func registerSection(section, fileName string) {
	sectionMu.Lock()
	defer sectionMu.Unlock()
	for _, e := range sectionRegistry {
		if e.section == section && e.fileName == fileName {
			return // already registered
		}
	}
	sectionRegistry = append(sectionRegistry, sectionEntry{section: section, fileName: fileName})
}

// WriteExperimentsIndex writes ./paper/include/sections/experiments.tex —
// a flat, auto-generated file that \InputIfFileExists every registered
// experiment section .tex, grouped by section.
func WriteExperimentsIndex() error {
	sectionMu.Lock()
	entries := make([]sectionEntry, len(sectionRegistry))
	copy(entries, sectionRegistry)
	sectionMu.Unlock()

	if len(entries) == 0 {
		return nil
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].section != entries[j].section {
			return entries[i].section < entries[j].section
		}
		return entries[i].fileName < entries[j].fileName
	})

	var sb strings.Builder
	sb.WriteString("% AUTO-GENERATED — do not edit by hand.\n")
	sb.WriteString("% Re-generated each time experiments run.\n\n")

	prevSection := ""
	for _, e := range entries {
		if e.section != prevSection {
			if prevSection != "" {
				sb.WriteString("\\FloatBarrier\n\\clearpage\n\n")
			}
			sb.WriteString("\\graphicspath{{include/" + e.section + "/}}\n")
			prevSection = e.section
		}
		sb.WriteString("\\InputIfFileExists{include/" + e.section + "/" + e.fileName + "}{}{}\n")
	}
	sb.WriteString("\\FloatBarrier\n\\clearpage\n")

	dir := PaperDir("sections")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("experiments index mkdir: %w", err)
	}

	out := filepath.Join(dir, "experiments.tex")
	if err := os.WriteFile(out, []byte(sb.String()), 0644); err != nil {
		return fmt.Errorf("experiments index write: %w", err)
	}

	return nil
}

type Reporter interface {
	WriteResults(tools.PipelineExperiment) error
	WriteArtifact(tools.PipelineExperiment, tools.Artifact) error
}

type ProjectorReporter struct {
}

type SnapshotReporter struct {
}

func NewProjectorReporter() *ProjectorReporter {
	return &ProjectorReporter{}
}

func NewSnapshotReporter() *SnapshotReporter {
	return &SnapshotReporter{}
}

func (reporter *ProjectorReporter) WriteResults(experiment tools.PipelineExperiment) error {
	return writeResultsSnapshot(experiment)
}

func (reporter *SnapshotReporter) WriteResults(experiment tools.PipelineExperiment) error {
	return writeResultsSnapshot(experiment)
}

func writeResultsSnapshot(experiment tools.PipelineExperiment) error {
	snapshot := map[string]any{
		"name":    experiment.Name(),
		"section": experiment.Section(),
		"results": experiment.TableData(),
		"artifacts": artifactMetadataList(
			experiment.Artifacts(),
		),
	}

	if scorer, ok := experiment.(interface{ Score() float64 }); ok {
		snapshot["score"] = scorer.Score()
	}

	return WriteJSONFile(
		snapshot,
		tools.Slugify(experiment.Name())+"_results.json",
		experiment.Section(),
	)
}

func (reporter *ProjectorReporter) WriteArtifact(experiment tools.PipelineExperiment, artifact tools.Artifact) error {
	if err := writeArtifactSnapshot(experiment, artifact); err != nil {
		return err
	}

	// Register self-contained section files for the experiments index.
	if artifact.Type == tools.ArtifactProse && strings.HasSuffix(artifact.FileName, "_section.tex") {
		registerSection(experiment.Section(), artifact.FileName)
	}

	switch artifact.Type {
	case tools.ArtifactTable:
		return WriteTable(artifact.Data, artifact.FileName, experiment.Section())
	case tools.ArtifactBarChart:
		data, err := barChartData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteBarChart(data.XAxis, data.Series, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactLineChart:
		data, err := lineChartData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteLineChart(data.XAxis, data.Series, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, data.YMin, data.YMax, experiment.Section())
	case tools.ArtifactComboChart:
		data, err := comboChartData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteComboChart(data.XAxis, data.Series, data.XName, data.YName, data.YMin, data.YMax, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactHeatMap:
		data, err := heatMapData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteHeatMap(data.XAxis, data.YAxis, data.Data, data.Min, data.Max, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactConfusionMatrix:
		data, err := confusionMatrixData(experiment, artifact.Data)
		if err != nil {
			return err
		}
		return WriteConfusionMatrix(data.Labels, data.Matrix, data.MeanScore, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactMultiPanel:
		data, err := multiPanelData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteMultiPanel(data.Panels, data.Width, data.Height, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactProse:
		data, err := proseData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteProse(data.Template, data.Data, artifact.FileName, experiment.Section())
	case tools.ArtifactImageStrip:
		data, err := imageStripData(artifact.Data)
		if err != nil {
			return err
		}
		return WriteImageStrip(data.Rows, artifact.Title, artifact.Caption, artifact.Label, artifact.FileName, experiment.Section())
	case tools.ArtifactPolarConstraint:
		data, err := polarConstraintData(artifact.Data)
		if err != nil {
			return err
		}
		return WritePolarConstraint(data, artifact.FileName, experiment.Section())
	default:
		return fmt.Errorf("unsupported artifact type %q", artifact.Type)
	}
}

func (reporter *SnapshotReporter) WriteArtifact(experiment tools.PipelineExperiment, artifact tools.Artifact) error {
	return writeArtifactSnapshot(experiment, artifact)
}

func writeArtifactSnapshot(experiment tools.PipelineExperiment, artifact tools.Artifact) error {
	if err := WriteJSONFile(
		artifactSnapshot(artifact),
		artifactJSONFileName(artifact.FileName),
		experiment.Section(),
	); err != nil {
		return err
	}

	return nil
}

func artifactSnapshot(artifact tools.Artifact) map[string]any {
	return map[string]any{
		"type":      artifact.Type,
		"file_name": artifact.FileName,
		"title":     artifact.Title,
		"caption":   artifact.Caption,
		"label":     artifact.Label,
		"data":      artifact.Data,
	}
}

func artifactMetadataList(artifacts []tools.Artifact) []map[string]any {
	out := make([]map[string]any, len(artifacts))
	for i, artifact := range artifacts {
		out[i] = map[string]any{
			"type":      artifact.Type,
			"file_name": artifact.FileName,
			"title":     artifact.Title,
			"caption":   artifact.Caption,
			"label":     artifact.Label,
		}
	}
	return out
}

func artifactJSONFileName(filename string) string {
	ext := filepath.Ext(filename)
	if ext != "" {
		filename = strings.TrimSuffix(filename, ext)
	}
	return filename + ".json"
}

func barChartData(data any) (tools.BarChartData, error) {
	switch typed := data.(type) {
	case tools.BarChartData:
		return typed, nil
	case *tools.BarChartData:
		if typed == nil {
			return tools.BarChartData{}, fmt.Errorf("bar chart data is nil")
		}
		return *typed, nil
	case []tools.ExperimentalData:
		series := []tools.BarSeries{
			{Name: "Exact", Data: extractScores(typed, "Exact")},
			{Name: "Partial", Data: extractScores(typed, "Partial")},
			{Name: "Fuzzy", Data: extractScores(typed, "Fuzzy")},
			{Name: "Weighted", Data: extractScores(typed, "Weighted")},
		}
		xAxis := make([]string, len(typed))
		for i, row := range typed {
			xAxis[i] = row.Name
		}
		return tools.BarChartData{XAxis: xAxis, Series: series}, nil
	default:
		return tools.BarChartData{}, fmt.Errorf("unexpected bar chart payload %T", data)
	}
}

func lineChartData(data any) (tools.LineChartData, error) {
	switch typed := data.(type) {
	case tools.LineChartData:
		return typed, nil
	case *tools.LineChartData:
		if typed == nil {
			return tools.LineChartData{}, fmt.Errorf("line chart data is nil")
		}
		return *typed, nil
	default:
		return tools.LineChartData{}, fmt.Errorf("unexpected line chart payload %T", data)
	}
}

func comboChartData(data any) (tools.ComboChartData, error) {
	switch typed := data.(type) {
	case tools.ComboChartData:
		return typed, nil
	case *tools.ComboChartData:
		if typed == nil {
			return tools.ComboChartData{}, fmt.Errorf("combo chart data is nil")
		}
		return *typed, nil
	default:
		return tools.ComboChartData{}, fmt.Errorf("unexpected combo chart payload %T", data)
	}
}

func heatMapData(data any) (tools.HeatMapData, error) {
	switch typed := data.(type) {
	case tools.HeatMapData:
		return typed, nil
	case *tools.HeatMapData:
		if typed == nil {
			return tools.HeatMapData{}, fmt.Errorf("heatmap data is nil")
		}
		return *typed, nil
	default:
		return tools.HeatMapData{}, fmt.Errorf("unexpected heatmap payload %T", data)
	}
}

func confusionMatrixData(experiment tools.PipelineExperiment, data any) (tools.ConfusionMatrixData, error) {
	switch typed := data.(type) {
	case tools.ConfusionMatrixData:
		return typed, nil
	case *tools.ConfusionMatrixData:
		if typed == nil {
			return tools.ConfusionMatrixData{}, fmt.Errorf("confusion matrix data is nil")
		}
		return *typed, nil
	case []tools.ExperimentalData:
		if predictor, ok := experiment.(interface{ ComputePredictions() }); ok {
			predictor.ComputePredictions()
		}

		labelProvider, ok := experiment.(interface{ ClassLabels() []string })
		if !ok {
			return tools.ConfusionMatrixData{}, fmt.Errorf("experiment %q does not expose class labels", experiment.Name())
		}

		labels := labelProvider.ClassLabels()
		matrix := make([][]int, len(labels))
		for i := range matrix {
			matrix[i] = make([]int, len(labels))
		}

		for _, row := range typed {
			if row.TrueLabel == nil || row.PredLabel == nil {
				continue
			}
			trueLabel := *row.TrueLabel
			predLabel := *row.PredLabel
			if trueLabel < 0 || trueLabel >= len(labels) || predLabel < 0 || predLabel >= len(labels) {
				continue
			}
			matrix[trueLabel][predLabel]++
		}

		payload := tools.ConfusionMatrixData{
			Labels: labels,
			Matrix: matrix,
		}

		if scorer, ok := experiment.(interface{ Score() float64 }); ok {
			payload.MeanScore = scorer.Score()
		}

		return payload, nil
	default:
		return tools.ConfusionMatrixData{}, fmt.Errorf("unexpected confusion matrix payload %T", data)
	}
}

func multiPanelData(data any) (tools.MultiPanelData, error) {
	switch typed := data.(type) {
	case tools.MultiPanelData:
		return typed, nil
	case *tools.MultiPanelData:
		if typed == nil {
			return tools.MultiPanelData{}, fmt.Errorf("multipanel data is nil")
		}
		return *typed, nil
	default:
		return tools.MultiPanelData{}, fmt.Errorf("unexpected multipanel payload %T", data)
	}
}

func proseData(data any) (tools.ProseData, error) {
	switch typed := data.(type) {
	case tools.ProseData:
		return typed, nil
	case *tools.ProseData:
		if typed == nil {
			return tools.ProseData{}, fmt.Errorf("prose data is nil")
		}
		return *typed, nil
	default:
		return tools.ProseData{}, fmt.Errorf("unexpected prose payload %T", data)
	}
}

func imageStripData(data any) (tools.ImageStripData, error) {
	switch typed := data.(type) {
	case tools.ImageStripData:
		return typed, nil
	case *tools.ImageStripData:
		if typed == nil {
			return tools.ImageStripData{}, fmt.Errorf("image strip data is nil")
		}
		return *typed, nil
	default:
		return tools.ImageStripData{}, fmt.Errorf("unexpected image strip payload %T", data)
	}
}

func polarConstraintData(data any) (projector.PolarConstraintData, error) {
	switch typed := data.(type) {
	case projector.PolarConstraintData:
		return typed, nil
	case *projector.PolarConstraintData:
		if typed == nil {
			return projector.PolarConstraintData{}, fmt.Errorf("polar constraint data is nil")
		}
		return *typed, nil
	default:
		return projector.PolarConstraintData{}, fmt.Errorf("unexpected polar constraint payload %T", data)
	}
}
