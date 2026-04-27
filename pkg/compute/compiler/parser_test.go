package compiler

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestCompile(t *testing.T) {
	Convey("Given the recruit_community source", t, func() {
		source := `
program recruit_community {
  ; Guard preventing the affinity from growing beyond the Shannon limit.
  (A.affinity > 120) {
    ; Set the current A value's status to DONE.
    set A.properties.status <- DONE

    ; Emit a fresh recruiter value with the same program as the current one.
    emit {
      set program <- A.program
    }
  }

  ; Pop the next B value from the community.
  pop(B) {
    (A.affinity == 0) {
      ; Seed the affinity with the current B value's affinity.
      write A.affinity <- B.affinity
    }

    ; Accumulate the B value's affinity into the A value's affinity.
    write A.affinity <- or(A.affinity, B.affinity)

    ; Set the current B value's community to the current A value's id.
    set B.properties.community <- A.id

    ; Set the current B value's status to DONE.
    set B.properties.status <- DONE
  }
}
`

		Convey("It should compile to a 16-word program without error", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			emitted := 0
			for _, word := range result.Words {
				if word != 0 {
					emitted++
				}
			}

			So(emitted, ShouldBeGreaterThan, 0)
			So(emitted, ShouldBeLessThanOrEqualTo, 16)
		})

		Convey("It should encode an emit instruction with the spawn flag set", func() {
			result, _ := Compile(source)

			foundEmit := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>54)&1 == 1 {
					foundEmit = true
				}
			}

			So(foundEmit, ShouldBeTrue)
		})

		Convey("It should encode at least one pop instruction with topology=Pop", func() {
			result, _ := Compile(source)

			foundPop := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>55)&3 == TopoPopQueue {
					foundPop = true
				}
			}

			So(foundPop, ShouldBeTrue)
		})

		Convey("It should encode at least one B-target instruction (target=1)", func() {
			result, _ := Compile(source)

			foundBTarget := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>53)&1 == 1 {
					foundBTarget = true
				}
			}

			So(foundBTarget, ShouldBeTrue)
		})

		Convey("It should encode a predicate instruction for each `(...)` block", func() {
			result, _ := Compile(source)

			predCount := 0
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>57)&1 == 1 {
					predCount++
				}
			}

			// The source has two predicated blocks: (A.affinity > 120)
			// and (A.affinity == 0).
			So(predCount, ShouldEqual, 2)
		})

		Convey("It should record constants for the integer literals and DONE enums", func() {
			result, _ := Compile(source)

			values := map[uint64]int{}
			for _, c := range result.Constants {
				values[c.Value]++
			}

			// 120 (outer guard threshold), 0 (inner == 0 threshold and
			// two fresh per-IF mask words), 4 (DONE, used twice).
			So(values[120], ShouldEqual, 1)
			So(values[4], ShouldEqual, 2)
			So(values[0], ShouldBeGreaterThanOrEqualTo, 1)
		})
	})
}

func BenchmarkCompile(b *testing.B) {
	source := `
program recruit_community {
  (A.affinity > 120) {
    set A.properties.status <- DONE
    emit { set program <- A.program }
  }
  pop(B) {
    (A.affinity == 0) {
      write A.affinity <- B.affinity
    }
    write A.affinity <- or(A.affinity, B.affinity)
    set B.properties.community <- A.id
    set B.properties.status <- DONE
  }
}
`

	for b.Loop() {
		_, _ = Compile(source)
	}
}
