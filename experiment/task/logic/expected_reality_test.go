package logic

import (
	"fmt"
	"testing"
	"unsafe"

	"math/rand"
	"os"
	"path/filepath"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/gpu/metal"
)

func randomChord() data.Chord {
	var c data.Chord
	// set ~20 random bits
	for i := 0; i < 20; i++ {
		c.Set(rand.Intn(512))
	}
	return c
}

func TestExpectedReality(t *testing.T) {
	Convey("Given a PrimeField with multiple diverging semantic paths and noise", t, func() {
		
		sizes := []int{1000, 10000, 100000}
		
		type scaleStat struct {
			Size          int
			BaseScore     float64
			SteeredScore  float64
			SteeredTarget int
			SteeredActual int
			Success       bool
		}
		
		var stats []scaleStat
		
		// Run synchronously to collect stats before Convey isolates the scope
		for _, size := range sizes {
			rand.Seed(int64(size))
			
			dictionary := make([]geometry.IcosahedralManifold, size)
			for i := 0; i < size; i++ {
				var multi geometry.IcosahedralManifold
				for j := 0; j < 8; j++ {
					multi.Cubes[0][0][j] = randomChord()[j]
				}
				dictionary[i] = multi
			}
			
			basePrimes := []int{100, 101, 102, 103, 104}
			pathXPrimes := append(basePrimes, 200, 201, 202) // Target 0
			pathYPrimes := append(basePrimes, 300, 301, 302) // Target 1
			pathZPrimes := append(basePrimes, 400, 401, 402) // Target 2
			
			targetIndices := []int{size / 4, size / 2, (size * 3) / 4}
			paths := [][]int{pathXPrimes, pathYPrimes, pathZPrimes}
			
			for i, primes := range paths {
				var chord data.Chord
				for _, p := range primes {
					chord.Set(p)
				}
				var multi geometry.IcosahedralManifold
				for j := 0; j < 8; j++ {
					multi.Cubes[0][0][j] = chord[j]
				}
				dictionary[targetIndices[i]] = multi
			}
			
			var ctxChord data.Chord
			for _, p := range basePrimes {
				ctxChord.Set(p)
			}
			var queryCtx geometry.IcosahedralManifold
			for i := 0; i < 8; i++ {
				queryCtx.Cubes[0][0][i] = ctxChord[i]
			}
			
			baseIdx, baseScore, _ := metal.BestFill(
				unsafe.Pointer(&dictionary[0]), size, unsafe.Pointer(&queryCtx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
			)
			
			var expectedChord data.Chord
			for _, p := range []int{400, 401, 402} { 
				expectedChord.Set(p)
			}
			var expectedReality geometry.IcosahedralManifold
			for i := 0; i < 8; i++ {
				expectedReality.Cubes[0][0][i] = expectedChord[i]
			}
			
			steeredIdx, steeredScore, _ := metal.BestFill(
				unsafe.Pointer(&dictionary[0]), size, unsafe.Pointer(&queryCtx), unsafe.Pointer(&expectedReality), 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
			)
			
			stats = append(stats, scaleStat{
				Size:          size,
				BaseScore:     baseScore,
				SteeredScore:  steeredScore,
				SteeredTarget: targetIndices[2],
				SteeredActual: steeredIdx,
				Success:       steeredIdx == targetIndices[2],
			})
			
			fmt.Printf("\n--- Active Inference @ Scale: %d ---\n", size)
			fmt.Printf("Control Winner: Index %d (Score %.4f)\n", baseIdx, baseScore)
			fmt.Printf("Steered Winner: Index %d (Score %.4f) [Target=%d]\n", steeredIdx, steeredScore, targetIndices[2])
		}
		
		Convey("When parsing the synchronous scaling stats", func() {
			Convey("Then the GPU successfully roots to the steered target across all sizes", func() {
				for _, stat := range stats {
					So(stat.Success, ShouldBeTrue)
				}
			})
			
			Convey("Artifacts should be generated for the paper", func() {
				var tableRows []map[string]any
				xAxis := make([]string, len(sizes))
				baseScores := make([]float64, len(sizes))
				steeredScores := make([]float64, len(sizes))
				
				for i, stat := range stats {
					xAxis[i] = fmt.Sprintf("%dK", stat.Size/1000)
					baseScores[i] = stat.BaseScore
					steeredScores[i] = stat.SteeredScore
					
					status := "Fail"
					if stat.Success {
						status = "Pass"
					}
					
					tableRows = append(tableRows, map[string]any{
						"DictionarySize": fmt.Sprintf("%d", stat.Size),
						"ControlScore":   fmt.Sprintf("%.3f", stat.BaseScore),
						"SteeredScore":   fmt.Sprintf("%.3f", stat.SteeredScore),
						"TargetIndex":    fmt.Sprintf("%d", stat.SteeredTarget),
						"FoundIndex":     fmt.Sprintf("%d", stat.SteeredActual),
						"Status":         status,
					})
				}
				
				So(WriteTable(tableRows, "expected_reality_scaling_table.tex"), ShouldBeNil)
				_, err := os.Stat(filepath.Join(PaperDir(), "expected_reality_scaling_table.tex"))
				So(err, ShouldBeNil)
				
				So(WriteBarChart(xAxis, []projector.BarSeries{
					{Name: "Control Score", Data: baseScores},
					{Name: "Steered Score", Data: steeredScores},
				}, "Active Inference Expected Reality Steering",
					"Resonance score across scale, control vs expected reality steering.",
					"fig:expected_reality_scaling", "expected_reality_scaling_chart"), ShouldBeNil)
			})
		})
	})
}
