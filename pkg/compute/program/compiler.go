package program

import (
	"context"
	"fmt"

	"github.com/theapemachine/six/pkg/compute/program/ts"
)

func Compile(source string, lay Layout) (Compiled, error) {
	var out Compiled

	words, err := compileFeedSource(source, lay)
	if err != nil {
		return out, err
	}

	out.Words = words

	return out, nil
}

func compileFeedSource(source string, lay Layout) ([]uint64, error) {
	srcBytes := []byte(source)

	program, err := ts.ParseFeedProgram(context.Background(), srcBytes)
	if err != nil {
		return nil, err
	}

	sitesTS := program.Sites
	sites := make([]feedSite, len(sitesTS))
	for idx := range sitesTS {
		sites[idx] = feedSite{
			emit:       sitesTS[idx].Emit,
			compact:    sitesTS[idx].CompactTerms,
			operations: sitesTS[idx].Operations,
		}
	}

	out := make([]uint64, 0, len(sites))

	if !program.HasFeed {
		var pendingPredStart, pendingPredCond uint64
		var incoming []feedAtom
		for site := 0; site < len(sites); site++ {
			if predStart, predCond, ok, err := feedSiteGate(sites[site], lay); err != nil {
				return nil, fmt.Errorf("site %d: %w", site+1, err)
			} else if ok {
				pendingPredStart = predStart
				pendingPredCond = predCond
				continue
			}

			words, result, ok, err := compileFeedSite(sites[site], pendingPredStart, pendingPredCond, incoming, lay)
			if err != nil {
				return nil, fmt.Errorf("site %d: %w", site+1, err)
			}
			if ok {
				out = append(out, words...)
				incoming = result
				pendingPredStart = 0
				pendingPredCond = 0
			}
		}

		return out, nil
	}

	var pendingPredStart, pendingPredCond uint64
	var incoming []feedAtom
	for site := len(sites) - 1; site >= 0; site-- {
		if predStart, predCond, ok, err := feedSiteGate(sites[site], lay); err != nil {
			return nil, fmt.Errorf("site %d: %w", len(sites)-site, err)
		} else if ok {
			pendingPredStart = predStart
			pendingPredCond = predCond
			continue
		}

		words, result, ok, err := compileFeedSite(sites[site], pendingPredStart, pendingPredCond, incoming, lay)
		if err != nil {
			return nil, fmt.Errorf("site %d: %w", len(sites)-site, err)
		}
		if ok {
			out = append(out, words...)
			incoming = result
			pendingPredStart = 0
			pendingPredCond = 0
		}
	}

	return out, nil
}
