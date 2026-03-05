package store

import (
	"math/bits"
	"time"
)

/*
SleepCycle is an asynchronous metabolic process that runs when the system
is idle or periodically. It simulates "sleep" by scanning the memory
substrate (the LSM tree) and orthogonalizing highly overlapping
(resonant) concepts to prevent catastrophic forgetting.
*/
func (idx *LSMSpatialIndex) SleepCycle(stopCh <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Second) // "Sleep" every 5 seconds for simulation
	defer ticker.Stop()

	for {
		select {
		case <-stopCh:
			return
		case <-ticker.C:
			idx.consolidateAndOrthogonalize()
		}
	}
}

/*
consolidateAndOrthogonalize performs the actual memory grooming.
It iterates over recent memory levels, finds chord pairs that have
high resonance (popcount > Threshold) but aren't identical, and
pushes them perfectly orthogonal by flipping their shared noise bits.
*/
func (idx *LSMSpatialIndex) consolidateAndOrthogonalize() {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	// If we don't have enough levels, skip
	if len(idx.levelsVals) == 0 {
		return
	}

	for lvl, vals := range idx.levelsVals {
		if len(vals) < 2 {
			continue
		}

		// Simple O(N) sweep across adjacent memory clusters in the level
		modified := false
		for i := 0; i < len(vals)-1; i++ {
			c1 := &vals[i]
			c2 := &vals[i+1]

			// Calculate resonance (shared bits)
			var shared int
			for j := range c1 {
				shared += bits.OnesCount64(c1[j] & c2[j])
			}

			// If they are highly resonant (e.g. share > 30% of active bits) but aren't identical
			if shared > 15 && shared < 40 {
				for j := range c1 {
					overlap := c1[j] & c2[j]
					if overlap > 0 {
						c2[j] &^= overlap
						modified = true
					}
				}
			}
		}

		if modified {
			// Memory was successfully compacted and made more distinct
			idx.levelsVals[lvl] = vals
		}
	}
}
