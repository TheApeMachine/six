package projector

import (
	"io"
	"math"
	"os"
)

/*
ConfusionMatrix renders a row-normalised confusion matrix PDF (with raw
counts, percentages, and an accuracy / F1 / resonance badge) and emits
a LaTeX figure stub.
*/
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

	spec := struct {
		Title     string   `json:"title"`
		Labels    []string `json:"labels"`
		Matrix    [][]int  `json:"matrix"`
		Accuracy  float64  `json:"accuracy"`
		MacroF1   float64  `json:"macro_f1"`
		Resonance float64  `json:"resonance"`
	}{cm.title, cm.labels, cm.matrix, acc, f1, cm.meanScore}

	if err := runPython("confusion", spec, cm.outDir, cm.filename); err != nil {
		return err
	}
	return emitFigure(cm.filename, cm.caption, cm.label, cm.out)
}

// metrics computes accuracy and macro-averaged F1 from the confusion matrix.
// Mismatched dimensions silently degrade to zeros — Python renderer paints
// a placeholder if the matrix is empty.
func (cm *ConfusionMatrix) metrics() (accuracy, macroF1 float64) {
	n := len(cm.labels)
	if n == 0 || len(cm.matrix) != n {
		return 0, 0
	}
	for _, row := range cm.matrix {
		if len(row) != n {
			return 0, 0
		}
	}

	total, correct := 0, 0
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

	f1Sum, valid := 0.0, 0
	for c := 0; c < n; c++ {
		tp := cm.matrix[c][c]
		fp, fn := 0, 0
		for i := 0; i < n; i++ {
			if i != c {
				fp += cm.matrix[i][c]
				fn += cm.matrix[c][i]
			}
		}
		var prec, rec float64
		if tp+fp > 0 {
			prec = float64(tp) / float64(tp+fp)
		}
		if tp+fn > 0 {
			rec = float64(tp) / float64(tp+fn)
		}
		var f float64
		if prec+rec > 0 {
			f = 2 * prec * rec / (prec + rec)
		}
		if !math.IsNaN(f) {
			f1Sum += f
			valid++
		}
	}
	if valid > 0 {
		macroF1 = f1Sum / float64(valid)
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
