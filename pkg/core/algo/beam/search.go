package beam

import (
	"context"

	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/errnie"
)

type Search struct {
	ctx        context.Context
	cancel     context.CancelFunc
	err        error
	prediction *algo.Prediction
	beamWidth  int
	maxHops    int
}

func NewSearch() *Search {
	return &Search{
		prediction: algo.NewPrediction(),
	}
}

func (search *Search) Update(
	prediction *algo.Prediction,
) (*algo.Prediction, error) {
	if err := validate.Require(map[string]any{
		"prediction": prediction,
	}); err != nil {
		return nil, errnie.Error(err)
	}

	search.prediction = prediction
	return search.prediction, nil
}

func (search *Search) Value() *algo.Prediction {
	return search.prediction
}
