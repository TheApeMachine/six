package phasedial

import tools "github.com/theapemachine/six/experiment"

// aphorismSplitPrompts returns half-and-half byte splits of the standard
// aphorism corpus for pipeline prompts and holdout scoring.
func aphorismSplitPrompts() (prompts []string, holdouts [][]byte) {
	for _, s := range tools.Aphorisms {
		p, h := tools.BytePrefixFraction(s, 0.5)
		if h == "" {
			continue
		}
		prompts = append(prompts, p)
		holdouts = append(holdouts, []byte(h))
	}
	return prompts, holdouts
}
