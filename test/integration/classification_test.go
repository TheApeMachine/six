package integration

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/primitive"
	"github.com/theapemachine/six/pkg/primitive/operation"
)

/*
reduceAND computes the corpus-wide AND by straightforward left fold.
*/
func reduceAND(values []*primitive.Value) *primitive.Value {
	if len(values) == 0 {
		return primitive.NewValue()
	}

	out := primitive.NewValue()
	*out = *values[0]

	for _, value := range values[1:] {
		operation.AND(out[:], value[:], out[:])
	}

	return out
}

/*
pairwiseANDLayer computes one tree-reduction layer of pairwise ANDs.
*/
func pairwiseANDLayer(values []*primitive.Value) []*primitive.Value {
	if len(values) == 0 {
		return nil
	}

	out := make([]*primitive.Value, 0, (len(values)+1)/2)

	for index := 0; index < len(values); index += 2 {
		if index+1 >= len(values) {
			carried := primitive.NewValue()
			*carried = *values[index]
			out = append(out, carried)
			continue
		}

		layerValue := primitive.NewValue()
		operation.AND(values[index][:], values[index+1][:], layerValue[:])
		out = append(out, layerValue)
	}

	return out
}

/*
andReductionLayers computes all pairwise AND layers up to the root.
*/
func andReductionLayers(values []*primitive.Value) [][]*primitive.Value {
	var layers [][]*primitive.Value

	current := values

	for len(current) > 1 {
		current = pairwiseANDLayer(current)
		layers = append(layers, current)
	}

	return layers
}

/*
orSummary computes the OR summary of a synthetic cluster.
*/
func orSummary(values []*primitive.Value) *primitive.Value {
	out := primitive.NewValue()

	for _, value := range values {
		operation.OR(out[:], value[:], out[:])
	}

	return out
}

/*
bestCluster returns the index of the summary with the highest GCD overlap.
*/
func bestCluster(query *primitive.Value, summaries []*primitive.Value) (int, int) {
	bestIndex := -1
	bestOverlap := -1

	for index, summary := range summaries {
		overlap := primitive.NewValue()
		operation.AND(query[:], summary[:], overlap[:])

		if overlap.PopCount() > bestOverlap {
			bestIndex = index
			bestOverlap = overlap.PopCount()
		}
	}

	return bestIndex, bestOverlap
}

func TestCorpusANDMatchesPairwiseTreeReduction(t *testing.T) {
	gc.Convey("Given a synthetic corpus with shared and cluster-local structure", t, func() {
		corpus := []*primitive.Value{
			valueWithBits(0, 1, 2),
			valueWithBits(0, 1, 3),
			valueWithBits(0, 4, 5),
			valueWithBits(0, 4, 6),
		}

		rootFromFold := reduceAND(corpus)
		layers := andReductionLayers(corpus)
		rootFromTree := layers[len(layers)-1][0]

		gc.So(rootFromTree.Equal(rootFromFold), gc.ShouldBeTrue)
		gc.So(rootFromTree.Equal(valueWithBits(0)), gc.ShouldBeTrue)
	})
}

func TestCancellationLayersMatchSyntheticCorpus(t *testing.T) {
	gc.Convey("Given a two-cluster synthetic corpus", t, func() {
		corpus := []*primitive.Value{
			valueWithBits(0, 1, 2),
			valueWithBits(0, 1, 3),
			valueWithBits(0, 4, 5),
			valueWithBits(0, 4, 6),
		}

		layers := andReductionLayers(corpus)
		firstLayer := layers[0]
		root := layers[1][0]

		gc.Convey("Pairwise reduction layers expose cluster-local and universal masks", func() {
			gc.So(firstLayer[0].Equal(valueWithBits(0, 1)), gc.ShouldBeTrue)
			gc.So(firstLayer[1].Equal(valueWithBits(0, 4)), gc.ShouldBeTrue)
			gc.So(root.Equal(valueWithBits(0)), gc.ShouldBeTrue)
		})

		gc.Convey("Stripping the universal layer leaves exact cluster-distinctive masks", func() {
			leftDistinctive := primitive.NewValue()
			rightDistinctive := primitive.NewValue()

			operation.AndNot(firstLayer[0][:], root[:], leftDistinctive[:])
			operation.AndNot(firstLayer[1][:], root[:], rightDistinctive[:])

			gc.So(leftDistinctive.Equal(valueWithBits(1)), gc.ShouldBeTrue)
			gc.So(rightDistinctive.Equal(valueWithBits(4)), gc.ShouldBeTrue)
		})
	})
}

func TestCancellationTerminatesWhenSharedPrimesDisappear(t *testing.T) {
	gc.Convey("Given stripped cluster-local masks with no remaining overlap", t, func() {
		left := valueWithBits(1)
		right := valueWithBits(4)
		shared := primitive.NewValue()

		operation.AND(left[:], right[:], shared[:])

		gc.So(shared.IsZero(), gc.ShouldBeTrue)
	})
}

func TestClassificationByMaxOverlapSelectsIntendedCluster(t *testing.T) {
	gc.Convey("Given explicit synthetic cluster summaries", t, func() {
		clusterA := orSummary([]*primitive.Value{
			valueWithBits(0, 1, 2),
			valueWithBits(0, 1, 3),
		})
		clusterB := orSummary([]*primitive.Value{
			valueWithBits(0, 4, 5),
			valueWithBits(0, 4, 6),
		})
		query := valueWithBits(0, 1, 7)

		bestIndex, bestOverlap := bestCluster(query, []*primitive.Value{clusterA, clusterB})

		gc.So(bestIndex, gc.ShouldEqual, 0)
		gc.So(bestOverlap, gc.ShouldEqual, 2)
	})
}

func TestDistinctiveClusterMasksAreExact(t *testing.T) {
	gc.Convey("Given two cluster summaries with one universal and separate distinctive structure", t, func() {
		clusterA := orSummary([]*primitive.Value{
			valueWithBits(0, 1, 2),
			valueWithBits(0, 1, 3),
		})
		clusterB := orSummary([]*primitive.Value{
			valueWithBits(0, 4, 5),
			valueWithBits(0, 4, 6),
		})

		onlyA := primitive.NewValue()
		onlyB := primitive.NewValue()

		operation.AndNot(clusterA[:], clusterB[:], onlyA[:])
		operation.AndNot(clusterB[:], clusterA[:], onlyB[:])

		gc.So(onlyA.Equal(valueWithBits(1, 2, 3)), gc.ShouldBeTrue)
		gc.So(onlyB.Equal(valueWithBits(4, 5, 6)), gc.ShouldBeTrue)
	})
}
