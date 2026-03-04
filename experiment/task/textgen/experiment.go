package textgen

import (
	"github.com/theapemachine/six/experiment/task"
	"github.com/theapemachine/six/vm"
)

/*
Experiment orchestrates the text generation experiments.
In runs the experiment and generates the paper artifacts.
*/
type Experiment struct {
	machine *vm.Machine
	test    string
	tests   map[string]task.Interface
}

type opts func(*Experiment)

func NewExperiment(opts ...opts) *Experiment {
	experiment := &Experiment{
		machine: vm.NewMachine(),
		tests: map[string]task.Interface{
			"completion": NewCompletion(),
		},
	}

	for _, opt := range opts {
		opt(experiment)
	}

	return experiment
}

func (experiment *Experiment) Run() {
	switch experiment.test {
	case "completion":
		experiment.tests[experiment.test].Run()
	default:
		for _, test := range experiment.tests {
			test.Run()
		}
	}
}

func WithTest(test string) opts {
	return func(experiment *Experiment) {
		experiment.test = test
	}
}
