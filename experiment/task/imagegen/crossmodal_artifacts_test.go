package imagegen

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"testing"
	"unsafe"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
	"github.com/theapemachine/six/experiment/projector"
	"github.com/theapemachine/six/geometry"
	"github.com/theapemachine/six/kernel"
	"github.com/theapemachine/six/store"
)

const pixelScale = 40

var (
	colorFG        = color.RGBA{30, 136, 229, 255}  // Blue: known filled pixel
	colorBG        = color.RGBA{245, 245, 245, 255} // Light grey: empty
	colorMasked    = color.RGBA{180, 180, 180, 255} // Grey: masked/unknown
	colorRecovered = color.RGBA{76, 175, 80, 255}   // Green: recovered via ChordHole
)

// maskStrategy defines which pixels are visible (true) vs masked (false).
type maskStrategy struct {
	name string
	// visible returns true if pixel (row,col) is part of the prompt.
	visible func(row, col int) bool
}

var masks = []maskStrategy{
	{"Top 3 rows", func(row, _ int) bool { return row < 3 }},
	{"Left 3 cols", func(_, col int) bool { return col < 3 }},
	{"Checkerboard", func(row, col int) bool { return (row+col)%2 == 0 }},
	{"Border only", func(row, col int) bool {
		return row == 0 || row == 6 || col == 0 || col == 4
	}},
	{"Random 50%", func(row, col int) bool {
		// Deterministic pseudo-random using a simple hash
		h := (row*7 + col*13 + 5) % 10
		return h < 5
	}},
}

func gridToPNG(grid [7][5]int, mask func(row, col int) color.RGBA) string {
	w, h := 5*pixelScale, 7*pixelScale
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	for row := 0; row < 7; row++ {
		for col := 0; col < 5; col++ {
			c := mask(row, col)
			for dy := 0; dy < pixelScale; dy++ {
				for dx := 0; dx < pixelScale; dx++ {
					img.SetRGBA(col*pixelScale+dx, row*pixelScale+dy, c)
				}
			}
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestCrossModalReconstructionArtifact(t *testing.T) {
	Convey("Given cross-modal image reconstruction with diverse masks", t, func() {

		// Use letter "A" as the target — it has the richest structure
		targetLetter := "A"
		img := binaryImages[targetLetter]

		// Store all 5 letters for the GPU to search
		letters := []string{"A", "T", "H", "L", "E"}
		pf := store.NewPrimeField()
		for i, name := range letters {
			pf.Insert(imageToChord(binaryImages[name], i))
		}

		Convey("When applying 5 different mask strategies", func() {

			var stripRows []projector.ImageStripRow

			for _, m := range masks {
				// Build prompt chord from visible pixels only
				var promptChord data.Chord
				for row := 0; row < 7; row++ {
					for col := 0; col < 5; col++ {
						if m.visible(row, col) && img[row][col] == 1 {
							promptChord.Set(row*5 + col)
						}
					}
				}

				var queryCtx geometry.IcosahedralManifold
				for i := 0; i < 8; i++ {
					queryCtx.Cubes[0][0][i] = promptChord[i]
				}

				bestIdx, _, err := kernel.BestFill(
					pf.Field(), pf.N, unsafe.Pointer(&queryCtx), nil, 0, unsafe.Pointer(&geometry.UnifiedGeodesicMatrix[0]),
				)
				So(err, ShouldBeNil)

				fullChord := pf.Manifold(bestIdx)
				hole := data.ChordHole(&fullChord.Cubes[0][0], &promptChord)

				// Render original
				origB64 := gridToPNG(img, func(row, col int) color.RGBA {
					if img[row][col] == 1 {
						return colorFG
					}
					return colorBG
				})

				// Render masked
				maskedB64 := gridToPNG(img, func(row, col int) color.RGBA {
					if m.visible(row, col) {
						if img[row][col] == 1 {
							return colorFG
						}
						return colorBG
					}
					return colorMasked
				})

				// Render reconstructed
				reconB64 := gridToPNG(img, func(row, col int) color.RGBA {
					if m.visible(row, col) {
						if img[row][col] == 1 {
							return colorFG
						}
						return colorBG
					}
					bitIdx := row*5 + col
					if hole.Has(bitIdx) {
						return colorRecovered
					}
					return colorBG
				})

				// Calculate accuracy on masked pixels
				total, correct := 0, 0
				for row := 0; row < 7; row++ {
					for col := 0; col < 5; col++ {
						if !m.visible(row, col) && img[row][col] == 1 {
							total++
							if hole.Has(row*5 + col) {
								correct++
							}
						}
					}
				}

				acc := 0.0
				if total > 0 {
					acc = float64(correct) / float64(total) * 100
				}

				stripRows = append(stripRows, projector.ImageStripRow{
					Original:      origB64,
					Masked:        maskedB64,
					Reconstructed: reconB64,
					Label: fmt.Sprintf(
						"Mask: %s — Match: %s — Recovery: %d/%d (%.0f%%)",
						m.name, letters[bestIdx], correct, total, acc,
					),
				})
			}

			Convey("Then the diverse-mask artifact is written", func() {
				err := WriteImageStrip(
					stripRows,
					"Cross-Modal Reconstruction: Diverse Mask Strategies",
					"Letter 'A' reconstructed under 5 mask strategies using pure BestFill popcount resonance. Blue: known pixels. Green: recovered via ChordHole. Grey: masked region.",
					"fig:crossmodal-diverse-masks",
					"crossmodal_reconstruction",
				)

				So(err, ShouldBeNil)
				fmt.Printf("\nWritten to: %s/crossmodal_reconstruction.{html,pdf}\n", PaperDir())
			})
		})
	})
}
