package experiment

import (
	"slices"

	. "github.com/smartystreets/goconvey/convey"
)

type BenchmarkType uint

const (
	ABOVERANDOM BenchmarkType = iota
)

var Aphorisms = []string{
	"Democracy requires individual sacrifice.",
	"Knowledge is power.",
	"Nature does not hurry, yet everything is accomplished.",
	"The only way to have a friend is to be one.",
	"To be, or not to be, that is the question.",
	"All happy families are alike; each unhappy family is unhappy in its own way.",
	"It was the best of times, it was the worst of times.",
	"In a hole in the ground there lived a hobbit.",
	"The sky above the port was the color of television, tuned to a dead channel.",
	"It is a truth universally acknowledged, that a single man in possession of a good fortune, must be in want of a wife.",
}

func Contains(slice []string, val string) bool {
	return slices.Contains(slice, val)
}

func Outcome(score float64, n int, benchmarkType BenchmarkType) (any, Assertion, any) {
	switch benchmarkType {
	case ABOVERANDOM:
		return score, ShouldBeGreaterThanOrEqualTo, 100.0/float64(n) + 0.05
	default:
		panic("unknown benchmark type")
	}
}

