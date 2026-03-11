package geometry

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/data"
)

func TestDualRotationCollisions(t *testing.T) {
	Convey("Given a set of byte sequences", t, func() {
		sequences := generateTestSequences()
		So(len(sequences), ShouldBeGreaterThan, 10)

		Convey("It should have fewer collisions with dual rotation than single", func() {
			type dualKey struct {
				R1 GFRotation
				R2 GFRotation
			}

			singleCollisions := 0
			orDualCollisions := 0
			boundDualCollisions := 0
			singleSeen := make(map[GFRotation]int)
			orDualSeen := make(map[dualKey]int)
			boundDualSeen := make(map[dualKey]int)

			for seqIdx, seq := range sequences {
				for prefixLen := 1; prefixLen <= len(seq); prefixLen++ {
					prefix := seq[:prefixLen]

					contentRot := IdentityRotation()
					boundRot := IdentityRotation()
					var orAccum data.Chord

					for pos, b := range prefix {
						chord := data.BaseChord(b)
						contentRot = contentRot.Compose(RotationForChord(chord))

						orAccum = data.ChordOR(&orAccum, &chord)

						rolled := chord.RollLeft((pos + 1) * 3)
						boundRot = boundRot.Compose(RotationForChord(rolled))
					}

					sk := contentRot

					orDK := dualKey{R1: contentRot, R2: RotationForChord(orAccum)}

					boundDK := dualKey{R1: contentRot, R2: boundRot}

					if prev, ok := singleSeen[sk]; ok && prev != seqIdx {
						singleCollisions++
					}
					singleSeen[sk] = seqIdx

					if prev, ok := orDualSeen[orDK]; ok && prev != seqIdx {
						orDualCollisions++
					}
					orDualSeen[orDK] = seqIdx

					if prev, ok := boundDualSeen[boundDK]; ok && prev != seqIdx {
						boundDualCollisions++
					}
					boundDualSeen[boundDK] = seqIdx
				}
			}

			Printf("\n  Total prefix rotations:    %d\n", len(singleSeen)+singleCollisions)
			Printf("  Single-key collisions:     %d (%.2f%%)\n", singleCollisions, 100*float64(singleCollisions)/float64(len(singleSeen)+singleCollisions))
			Printf("  OR-dual collisions:        %d (%.2f%%)\n", orDualCollisions, 100*float64(orDualCollisions)/float64(len(orDualSeen)+orDualCollisions))
			Printf("  Bound-dual collisions:     %d (%.2f%%)\n", boundDualCollisions, 100*float64(boundDualCollisions)/float64(len(boundDualSeen)+boundDualCollisions))

			So(boundDualCollisions, ShouldBeLessThan, singleCollisions)
		})
	})
}

func generateTestSequences() [][]byte {
	sequences := [][]byte{
		[]byte("Hello World"),
		[]byte("Goodbye Moon"),
		[]byte("from typing import List"),
		[]byte("def has_close_elements(numbers):"),
		[]byte("for idx, elem in enumerate(numbers):"),
		[]byte("distance = abs(elem - elem2)"),
		[]byte("if distance < threshold:"),
		[]byte("return True"),
		[]byte("return False"),
		[]byte("#include <vector>"),
		[]byte("using namespace std;"),
		[]byte("int main() { return 0; }"),
		[]byte("import java.util.*;"),
		[]byte("public class Main {"),
		[]byte("System.out.println(result);"),
		[]byte("func main() {"),
		[]byte("fmt.Println(result)"),
		[]byte("fn main() {}"),
		[]byte("let result = vec![1, 2, 3];"),
		[]byte("const hasCloseElements = (numbers) => {"),
	}

	for i := 0; i < 40; i++ {
		seq := make([]byte, 100+i*10)

		for j := range seq {
			seq[j] = byte((i*7 + j*13 + 17) % 256)
		}

		sequences = append(sequences, seq)
	}

	return sequences
}

func BenchmarkDualRotationDerivation(b *testing.B) {
	seq := make([]byte, 500)

	for i := range seq {
		seq[i] = byte((i * 13) % 256)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		contentRot := IdentityRotation()
		var orAccum data.Chord

		for _, by := range seq {
			chord := data.BaseChord(by)
			contentRot = contentRot.Compose(RotationForChord(chord))
			orAccum = data.ChordOR(&orAccum, &chord)
		}

		_ = RotationForChord(orAccum)
	}
}
