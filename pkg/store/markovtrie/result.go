package markovtrie

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

/*
Prediction is the unified response from Predict. The caller passes data in
and gets back classification and continuations — nothing else leaks out.
*/
type Prediction struct {
	Label         string
	Confidence    float64
	Scores        map[string]float64
	Continuations []BeamCandidate
}
