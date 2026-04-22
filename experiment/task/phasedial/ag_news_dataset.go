package phasedial

import (
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
)

var agNewsLabels = []string{"world", "sports", "business", "sci_tech"}

type AGNewsDatasetBuilder struct {
	samples uint32
	split   string
}

func NewAGNewsDatasetBuilder() *AGNewsDatasetBuilder {
	return &AGNewsDatasetBuilder{
		samples: 256,
		split:   "test",
	}
}

func (builder *AGNewsDatasetBuilder) WithSamples(samples uint32) *AGNewsDatasetBuilder {
	if samples > 0 {
		builder.samples = samples
	}

	return builder
}

func (builder *AGNewsDatasetBuilder) WithSplit(split string) *AGNewsDatasetBuilder {
	if split != "" {
		builder.split = split
	}

	return builder
}

func (builder *AGNewsDatasetBuilder) Build() data.Provider {
	return huggingface.New(
		huggingface.DatasetWithRepo("sh0416/ag_news"),
		huggingface.DatasetWithSamples(int(builder.samples)),
		huggingface.DatasetWithSplit(builder.split),
		huggingface.DatasetWithTextColumns("title", "description"),
		huggingface.DatasetWithLabelColumn("label"),
		huggingface.DatasetWithLabelAppend(agNewsLabels),
		// ag_news labels are 1-indexed (1=World, 2=Sports, 3=Business,
		// 4=Sci/Tech). Normalizing at the source means downstream
		// experiments can index agNewsLabels directly.
		huggingface.DatasetWithLabelOrigin(1),
	)
}

// func NewTorusGeneralizationAGNewsExperiment(
// 	samples uint32, opts ...torusGeneralizationOpt,
// ) *TorusGeneralizationExperiment {
// 	agNewsDataset := NewAGNewsDatasetBuilder().WithSamples(samples).Build()

// 	combinedOpts := append(
// 		[]torusGeneralizationOpt{TorusGeneralizationWithDataset(agNewsDataset)}, opts...,
// 	)

// 	return NewTorusGeneralizationExperiment(combinedOpts...)
// }

func NewSteerabilityAGNewsExperiment(samples uint32) *SteerabilityExperiment {
	agNewsDataset := NewAGNewsDatasetBuilder().WithSamples(samples).Build()

	return NewSteerabilityExperiment(
		SteerabilityWithDataset(agNewsDataset),
	)
}

