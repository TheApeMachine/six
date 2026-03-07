package experiment

import (
	"github.com/theapemachine/six/provider"
	"github.com/theapemachine/six/provider/local"
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

func NewLocalProvider(corpus []string) provider.Dataset {
	data := make([][]byte, len(corpus))
	for i, s := range corpus {
		data[i] = []byte(s)
	}
	return local.New(data)
}

func Contains(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}
