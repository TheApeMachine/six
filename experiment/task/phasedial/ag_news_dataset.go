package phasedial

import (
	"github.com/theapemachine/six/experiment/data"
	"github.com/theapemachine/six/experiment/data/huggingface"
)

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
		// ag_news ships 1-indexed labels (1=World..4=Sci/Tech). Normalizing
		// at the source means everything downstream sees a 0-indexed class
		// id; the substrate re-shifts to keep 0 as the unlabeled sentinel.
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
