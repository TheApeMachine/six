package train

import (
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core/algo"
	"github.com/theapemachine/six/pkg/primitive"
)

func TestOnlineUpdate(t *testing.T) {
	t.Parallel()

	Convey("Update returns early when prediction nil", t, func() {
		online := NewOnline()

		out, err := online.Update(nil)

		So(err, ShouldBeNil)
		So(out, ShouldEqual, online.prediction)
	})

	Convey("Update returns early without labels", t, func() {
		online := NewOnline()
		prediction := algo.NewPrediction()

		v, err := primitive.NewValue([]byte("x"))

		So(err, ShouldBeNil)

		defer v.Close()

		prediction.Context = append(prediction.Context, *v)

		out, err := online.Update(prediction)

		So(err, ShouldBeNil)
		So(out, ShouldEqual, online.prediction)
	})

	Convey("Update advances step and class totals", t, func() {
		online := NewOnline()
		prediction := algo.NewPrediction()
		prediction.Labels = append(prediction.Labels, algo.Label{
			Label:      []byte("classA"),
			Confidence: 1,
		})

		v, err := primitive.NewValue([]byte("token"))

		So(err, ShouldBeNil)

		defer v.Close()

		prediction.Context = append(prediction.Context, *v)

		before := online.CurrentStep

		_, err = online.Update(prediction)

		So(err, ShouldBeNil)
		So(online.CurrentStep, ShouldEqual, before+1)
		So(online.ClassTotals["classA"], ShouldBeGreaterThan, 0)
	})
}

func TestOnlineUpdateConcurrent(t *testing.T) {
	t.Parallel()

	Convey("Update from many goroutines keeps class totals and step coherent", t, func() {
		online := NewOnline()

		const workers = 64
		const rounds = 50

		var wait sync.WaitGroup

		wait.Add(workers)

		for worker := 0; worker < workers; worker++ {
			go func() {
				defer wait.Done()

				var stub primitive.Value

				for round := 0; round < rounds; round++ {
					prediction := algo.NewPrediction()
					prediction.Labels = append(prediction.Labels, algo.Label{
						Label:      []byte("L"),
						Confidence: 1,
					})
					prediction.Context = append(prediction.Context, stub)

					_, _ = online.Update(prediction)
				}
			}()
		}

		wait.Wait()

		So(online.CurrentStep, ShouldEqual, workers*rounds)
		So(online.ClassTotals["L"], ShouldBeGreaterThan, 0)
		So(len(online.Labels), ShouldEqual, 1)
	})
}

func TestOnlineStep(t *testing.T) {
	t.Parallel()

	Convey("Step ignores blank labels", t, func() {
		online := NewOnline()
		var stub primitive.Value

		step := online.CurrentStep

		online.Step("  ", 1, stub)

		So(online.CurrentStep, ShouldEqual, step)
	})

	Convey("Step ignores non-positive learning rate", t, func() {
		online := NewOnline()
		var stub primitive.Value

		step := online.CurrentStep

		online.Step("ok", 0, stub)

		So(online.CurrentStep, ShouldEqual, step)
	})
}

func TestOnlineLearningRate(t *testing.T) {
	t.Parallel()

	Convey("LearningRate starts at derived default", t, func() {
		online := NewOnline()

		So(online.LearningRate(), ShouldEqual, 1.0)
	})
}

func TestOnlineAddLabel(t *testing.T) {
	t.Parallel()

	Convey("AddLabel deduplicates entries", t, func() {
		online := NewOnline()

		online.AddLabel("z")
		online.AddLabel("z")

		So(online.Labels, ShouldResemble, []string{"z"})
	})
}

func TestOnlineNextConceptLabel(t *testing.T) {
	t.Parallel()

	Convey("NextConceptLabel increments counter", t, func() {
		online := NewOnline()

		first := online.NextConceptLabel()
		second := online.NextConceptLabel()

		So(first, ShouldNotEqual, second)
	})
}

func TestOnlineValue(t *testing.T) {
	t.Parallel()

	Convey("Value exposes canonical prediction", t, func() {
		online := NewOnline()

		So(online.Value().Signals[algo.Surprisal], ShouldNotBeNil)
	})
}

func BenchmarkOnlineUpdate(b *testing.B) {
	online := NewOnline()
	payload, err := primitive.NewValue([]byte("training text"))

	if err != nil {
		b.Fatal(err)
	}

	defer payload.Close()

	prediction := algo.NewPrediction()
	prediction.Labels = append(prediction.Labels, algo.Label{
		Label:      []byte("lbl"),
		Confidence: 1,
	})
	prediction.Context = append(prediction.Context, *payload)

	b.ResetTimer()

	for b.Loop() {
		_, _ = online.Update(prediction)
	}
}
