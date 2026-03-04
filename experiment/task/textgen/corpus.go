package textgen

import "strings"

func proseCorpus() []string {
	return []string{
		"The quick brown fox jumps over the lazy dog.",
		"To be, or not to be, that is the question: Whether 'tis nobler in the mind to suffer The slings and arrows of outrageous fortune, Or to take arms against a sea of troubles And by opposing end them.",
		"All happy families are alike; each unhappy family is unhappy in its own way.",
		"It was the best of times, it was the worst of times, it was the age of wisdom, it was the age of foolishness.",
		"Call me Ishmael. Some years ago—never mind how long precisely—having little or no money in my purse, and nothing particular to interest me on shore, I thought I would sail about a little and see the watery part of the world.",
		"In a hole in the ground there lived a hobbit. Not a nasty, dirty, wet hole, filled with the ends of worms and an oozy smell, nor yet a dry, bare, sandy hole with nothing in it to sit down on or to eat: it was a hobbit-hole, and that means comfort.",
		"The sky above the port was the color of television, tuned to a dead channel.",
		"It is a truth universally acknowledged, that a single man in possession of a good fortune, must be in want of a wife.",
	}
}

// tokenize splits text into simple whitespace tokens.
func tokenize(text string) []string {
	words := strings.Fields(text)
	var tokens []string
	for _, w := range words {
		tokens = append(tokens, w)
	}
	return tokens
}

// detokenize joins tokens back into text.
func detokenize(tokens []string) string {
	return strings.Join(tokens, " ")
}
