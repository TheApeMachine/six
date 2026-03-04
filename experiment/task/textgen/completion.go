package textgen

import (
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/experiment/task"
)

type Completion struct {
	task.Interface
}

func NewCompletion() *Completion {
	return &Completion{}
}

func (completion *Completion) Run() error {
	// For example purposes, we pass some synthetic data to the generic table projector
	data := []map[string]any{
		{"Model": "Baseline", "Score": 85.0, "Time (s)": 1.2},
		{"Model": "Phase Dial", "Score": 92.5, "Time (s)": 1.5},
	}

	table := projector.NewTable(
		projector.TableWithData(data),
	)

	section := projector.NewSection(
		projector.SectionWithTitle("Text Generation Performance"),
		projector.SectionWithContent("In this section we evaluate the context completion capabilities of the model. The table below outlines the results comparing the baseline implementation against the phase dial navigation mechanism."),
		projector.SectionWithElements(table),
	)

	return section.Generate()
}
