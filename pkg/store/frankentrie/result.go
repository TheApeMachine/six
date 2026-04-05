package frankentrie

/*
ReplayResult reports one accepted replay sequence.
*/
type ReplayResult struct {
	Sequence   string
	Label      string
	Confidence float64
}

/*
ExperienceResult captures one unsupervised or supervised plasticity update.
The learning rate follows surprise-modulated plasticity when training is driven
by Experience.
*/
type ExperienceResult struct {
	Label        string
	Surprisal    float64
	LearningRate float64
	IsNewConcept bool
}
