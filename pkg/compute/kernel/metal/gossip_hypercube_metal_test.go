//go:build darwin && cgo

package metal

import (
	"testing"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

// TestHypercubeGossip_MatchesCPUReference checks Metal hypercube_gossip_kernel
// against kernel.HypercubeGossipRef: full 256-Value simd+threadgroup path, and
// the partial-count threadgroup path (including XOR with missing hypercube
// partners — neutral zero, not self-fold).
func TestHypercubeGossip_MatchesCPUReference(t *testing.T) {
	if Available() == 0 || !metalReady.Load() {
		t.Skip("metal backend unavailable")
	}

	primitive.EnsureArenaPinnedForGPU()
	if err := ensureMetalArena(); err != nil {
		t.Fatalf("ensureMetalArena: %v", err)
	}

	cases := []struct {
		name string
		n    int
		op   kernel.GossipOp
		dMax uint8
	}{
		{"256_XOR_full", 256, kernel.GossipOpXOR, 8},
		{"256_OR_full", 256, kernel.GossipOpOR, 8},
		{"7_XOR_partial", 7, kernel.GossipOpXOR, 0},
		{"5_OR_partial", 5, kernel.GossipOpOR, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bandW := kernel.GossipBandWords

			values := make([]*primitive.Value, 0, tc.n)
			backup := make([][]uint64, tc.n)

			for i := 0; i < tc.n; i++ {
				v := primitive.AllocValue()
				values = append(values, v)
				backup[i] = make([]uint64, bandW)
				src := (*v)[kernel.GossipBandStartWord : kernel.GossipBandStartWord+bandW]
				for w := 0; w < bandW; w++ {
					// Distinct, deterministic pattern per (value, word).
					src[w] = uint64(i+1)*(uint64(w+1)+0x9E37_79B9) ^ 0xA5A5_5A5A
				}
				copy(backup[i], src)
			}

			t.Cleanup(func() {
				for _, v := range values {
					primitive.FreeValue(v)
				}
			})

			dmax := tc.dMax
			if dmax == 0 {
				dmax = kernel.HypercubeDimensions(tc.n)
			}

			run := func(apply func([]*primitive.Value)) {
				for i, v := range values {
					dst := (*v)[kernel.GossipBandStartWord : kernel.GossipBandStartWord+bandW]
					copy(dst, backup[i])
				}
				apply(values)
			}

			assetCPU := make([][]uint64, tc.n)
			run(func(vs []*primitive.Value) {
				kernel.PublishGossipBand(vs)
				kernel.HypercubeGossipRef(vs, tc.op, dmax)
				for i, v := range vs {
					assetCPU[i] = make([]uint64, bandW)
					copy(assetCPU[i], (*v)[kernel.GossipBandTargetWord:kernel.GossipBandTargetWord+bandW])
				}
			})

			indices, ok := collectArenaIndices(values)
			if !ok {
				t.Fatal("not all arena values")
			}
			if len(indices) != tc.n {
				t.Fatalf("indices len %d want %d", len(indices), tc.n)
			}

			run(func(vs []*primitive.Value) {
				err := hypercubeGossipDispatch(indices, uint32(dmax), foldOpToInt(tc.op))
				if err != nil {
					t.Fatalf("hypercubeGossipDispatch: %v", err)
				}
			})

			for i, v := range values {
				got := (*v)[kernel.GossipBandTargetWord : kernel.GossipBandTargetWord+bandW]
				for w := 0; w < bandW; w++ {
					if got[w] != assetCPU[i][w] {
						t.Fatalf("value i=%d word w=%d got=%#x want=%#x", i, w, got[w], assetCPU[i][w])
					}
				}
			}
		})
	}
}
