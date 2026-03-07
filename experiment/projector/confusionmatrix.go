package projector

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
)

//go:embed confusionmatrix_script.tmpl
var confusionMatrixScriptTmpl string

// ConfusionMatrix renders an ECharts confusion-matrix heatmap to HTML+PDF
// and emits a LaTeX figure stub.  The visual style matches the publication
// convention: row-normalised blue colour scale, cell counts with percentages,
// and an accuracy / macro-F1 badge.
type ConfusionMatrix struct {
	out       io.Writer
	title     string
	labels    []string
	matrix    [][]int // matrix[true][predicted] = count
	caption   string
	label     string
	filename  string
	outDir    string
	meanScore float64
}

type confusionMatrixOpts func(*ConfusionMatrix)

func NewConfusionMatrix(opts ...confusionMatrixOpts) *ConfusionMatrix {
	cm := &ConfusionMatrix{out: os.Stdout, filename: "confusion_matrix", outDir: "."}
	for _, opt := range opts {
		opt(cm)
	}
	return cm
}

func (cm *ConfusionMatrix) SetOutput(out io.Writer) { cm.out = out }

func (cm *ConfusionMatrix) Generate() error {
	acc, f1 := cm.metrics()
	labelsJSON, _ := json.Marshal(cm.labels)
	matrixJSON, _ := json.Marshal(cm.matrix)

	script := execTemplate(confusionMatrixScriptTmpl, struct {
		LabelsJSON string
		MatrixJSON string
		Accuracy   string
		MacroF1    string
		Resonance  string
	}{
		LabelsJSON: string(labelsJSON),
		MatrixJSON: string(matrixJSON),
		Accuracy:   fmt.Sprintf("%.6f", acc),
		MacroF1:    fmt.Sprintf("%.6f", f1),
		Resonance:  fmt.Sprintf("%.6f", cm.meanScore),
	})

	html, err := renderChartHTML(cm.title, chartW, chartH, script)
	if err != nil {
		return err
	}
	if err := renderAndExport(html, cm.outDir, cm.filename, chartW, chartH); err != nil {
		return err
	}
	return emitFigure(cm.filename, cm.caption, cm.label, cm.out)
}

// metrics computes accuracy and macro-averaged F1 from the confusion matrix.
func (cm *ConfusionMatrix) metrics() (accuracy, macroF1 float64) {
	n := len(cm.labels)
	if n == 0 {
		return 0, 0
	}

	total := 0
	correct := 0
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			total += cm.matrix[i][j]
			if i == j {
				correct += cm.matrix[i][j]
			}
		}
	}

	if total > 0 {
		accuracy = float64(correct) / float64(total)
	}

	// Per-class precision, recall, F1
	f1Sum := 0.0
	validClasses := 0
	for c := 0; c < n; c++ {
		tp := cm.matrix[c][c]
		fp := 0
		fn := 0
		for i := 0; i < n; i++ {
			if i != c {
				fp += cm.matrix[i][c] // others predicted as c
				fn += cm.matrix[c][i] // c predicted as others
			}
		}
		prec := 0.0
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		rec := 0.0
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		f := 0.0
		if prec+rec > 0 {
			f = 2 * prec * rec / (prec + rec)
		}
		if !math.IsNaN(f) {
			f1Sum += f
			validClasses++
		}
	}
	if validClasses > 0 {
		macroF1 = f1Sum / float64(validClasses)
	}
	return accuracy, macroF1
}

// --- Functional options ---

// ConfusionMatrixWithData sets the class labels and the count matrix.
// matrix[trueClass][predictedClass] = count.
func ConfusionMatrixWithData(labels []string, matrix [][]int) confusionMatrixOpts {
	return func(cm *ConfusionMatrix) {
		cm.labels = labels
		cm.matrix = matrix
	}
}

func ConfusionMatrixWithMeta(title, caption, label string) confusionMatrixOpts {
	return func(cm *ConfusionMatrix) {
		cm.title = title
		cm.caption = caption
		cm.label = label
	}
}

func ConfusionMatrixWithOutput(outDir, filename string) confusionMatrixOpts {
	return func(cm *ConfusionMatrix) {
		cm.outDir = outDir
		cm.filename = filename
	}
}

func ConfusionMatrixWithMeanScore(score float64) confusionMatrixOpts {
	return func(cm *ConfusionMatrix) {
		cm.meanScore = score
	}
}
