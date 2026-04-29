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

func TestCompileBPredicate(t *testing.T) {
	Convey("Given a query source that predicates on popped B state", t, func() {
		source := `
program query {
  pop(B) {
    (B.properties.community == 0) {
      write B.properties.reference <- A.properties.reference
      stage(B)
    }
  }
}
`

		Convey("It should encode the predicate with SrcAFromB", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			found := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>57)&1 == 1 && (word>>61)&1 == 1 {
					found = true
					break
				}
			}

			So(found, ShouldBeTrue)
		})
	})
}

func TestCompileRot8(t *testing.T) {
	Convey("Given source that rotates the B operand by byte steps", t, func() {
		source := `
program align {
  pop(B) {
    write A.signals[0,2] <- xor(A.tokens[0,2], rot8(B.tokens[0,2], 3))
  }
}
`

		Convey("It should encode rot8 into the truth-table operand metadata", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			found := false
			for _, word := range result.Words {
				if word == 0 || word&0xF != OpXor {
					continue
				}

				So(word>>57&1, ShouldEqual, 0)
				So(word>>58&7, ShouldEqual, 3)
				found = true
			}

			So(found, ShouldBeTrue)
		})
	})

	Convey("Given source that directly copies a rotated B operand", t, func() {
		source := `
program align {
  write A.signals[0,1] <- rot8(B.tokens[0,2], 1)
}
`

		Convey("It should encode rot8 on the copy-B instruction", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			found := false
			for _, word := range result.Words {
				if word == 0 || word&0xF != OpCopyB {
					continue
				}

				So(word>>57&1, ShouldEqual, 0)
				So(word>>58&7, ShouldEqual, 1)
				found = true
			}

			So(found, ShouldBeTrue)
		})
	})

	Convey("Given source that rotates B inside a predicate expression", t, func() {
		source := `
program align {
  (popcnt(xor(A.tokens[0,2], rot8(B.tokens[0,2], 2))) < 64) {
    write A.signals[0,1] <- A.tokens[0,1]
  }
}
`

		Convey("It should materialize the rotated truth-table expression before the predicate", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			foundRotatedXor := false
			foundPredicate := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := word >> 57 & 1
				condOrRotate := word >> 58 & 7

				if opcode == OpXor && predicate == 0 && condOrRotate == 2 {
					foundRotatedXor = true
				}

				if predicate == 1 && condOrRotate == PredLT {
					foundPredicate = true
				}
			}

			So(foundRotatedXor, ShouldBeTrue)
			So(foundPredicate, ShouldBeTrue)
		})
	})

	Convey("Given source that rotates A", t, func() {
		source := `
program align {
  write A.signals[0,1] <- rot8(A.tokens[0,1], 1)
}
`

		Convey("It should reject non-B operand rotation", func() {
			_, err := Compile(source)
			So(err, ShouldNotBeNil)
		})
	})
}

func BenchmarkCompileRot8(b *testing.B) {
	source := `
program align {
  pop(B) {
    write A.signals[0,2] <- xor(A.tokens[0,2], rot8(B.tokens[0,2], 3))
  }
}
`

	b.ReportAllocs()
	for b.Loop() {
		_, _ = Compile(source)
	}
}

func TestCompileHammingPredicate(t *testing.T) {
	Convey("Given the current recruit_community source", t, func() {
		source := `
program recruit_community {
  pop(B) {
    (A.context[0,5] == 0) {
      write A.context[0,5] <- B.affinity
    }

    (popcnt(or(A.affinity, B.affinity)) <= 121) {
      (popcnt(xor(A.context[0,5], B.affinity)) < 64) {
        write A.affinity <- or(A.affinity, B.affinity)
        set B.properties.community <- A.id
      }
    }
  }

  (popcnt(A.affinity) >= 121) {
    emit {
      set program <- A.program
      set A.affinity <- 0
      set A.context[0,5] <- 0
    }
  }
}
`

		Convey("It should lower the Hamming distance predicate into scratch xor plus popcount compare", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			foundXor := false
			foundPredicate := false

			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				cond := (word >> 58) & 7
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1

				if opcode == OpXor && aStart == contextStart && aSpan == affinityWords && dstStart >= assetConstStart && dstSpan == affinityWords {
					foundXor = true
				}

				if predicate == 1 && cond == PredLT && aStart >= assetConstStart && aSpan == 1 {
					foundPredicate = true
				}
			}

			So(foundXor, ShouldBeTrue)
			So(foundPredicate, ShouldBeTrue)
		})

		Convey("It should reject candidates whose post-write union exceeds the Shannon limit", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			foundUnionOr := false
			foundUnionPredicate := false

			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				opcode := word & 0xF
				predicate := (word >> 57) & 1
				cond := (word >> 58) & 7
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				dstStart := (word >> 32) & 0x7F
				dstSpan := ((word >> 39) & 0x7F) + 1

				if opcode == OpOr && aStart == affinityStart && aSpan == affinityWords && dstStart >= assetConstStart && dstSpan == affinityWords {
					foundUnionOr = true
				}

				if predicate == 1 && cond == PredLE && aStart >= assetConstStart && aSpan == 1 {
					foundUnionPredicate = true
				}
			}

			So(foundUnionOr, ShouldBeTrue)
			So(foundUnionPredicate, ShouldBeTrue)
		})

		Convey("It should guard the nested predicate with the outer saturation mask", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			foundNestedPredicate := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				predicate := (word >> 57) & 1
				cond := (word >> 58) & 7
				aStart := (word >> 4) & 0x7F
				aSpan := ((word >> 11) & 0x7F) + 1
				maskStart := (word >> 46) & 0x7F

				if predicate == 1 && cond == PredLT && aStart >= assetConstStart && aSpan == 1 && maskStart != MaskTrue.Start {
					foundNestedPredicate = true
				}
			}

			So(foundNestedPredicate, ShouldBeTrue)
		})

		Convey("It should emit only one child for the multi-instruction emit body", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			emits := 0
			for _, word := range result.Words {
				if word != 0 && (word>>54)&1 == 1 {
					emits++
				}
			}

			So(emits, ShouldEqual, 1)
		})
	})
}

func TestCompileReducers(t *testing.T) {
	Convey("Given generic lane reducer source", t, func() {
		source := `
program reducers {
  set A.properties.community <- argmin_nonzero(B.properties.community, B.properties.surprisal)
  set A.properties.labels <- mode_eq(B.properties.labels, B.properties.community, A.properties.community)
  set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature)
}
`

		Convey("It should lower reducers without task-specific operands", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			argmin := result.Words[0]
			mode := result.Words[1]
			zipf := result.Words[2]

			So(argmin&0xF, ShouldEqual, OpReduceArgMinNonZero)
			So(argmin>>57&1, ShouldEqual, 1)
			So(argmin>>61&1, ShouldEqual, 1)

			So(mode&0xF, ShouldEqual, OpReduceModeEq)
			So(mode>>46&0x7F, ShouldEqual, PropertyOffsets["community"])
			So(mode>>57&1, ShouldEqual, 1)
			So(mode>>61&1, ShouldEqual, 1)

			So(zipf&0xF, ShouldEqual, OpReduceZipfSelect)
			So(zipf>>46&0x7F, ShouldEqual, PropertyOffsets["temperature"])
			So(zipf>>57&1, ShouldEqual, 1)
			So(zipf>>61&1, ShouldEqual, 1)
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

func BenchmarkCompileReducers(b *testing.B) {
	source := `
program reducers {
  set A.properties.community <- argmin_nonzero(B.properties.community, B.properties.surprisal)
  set A.properties.labels <- mode_eq(B.properties.labels, B.properties.community, A.properties.community)
  set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature)
}
`

	for b.Loop() {
		_, _ = Compile(source)
	}
}

/*
TestCompileIndirectOperands pins down the contract for `B.{{addr}}` /
`{{addr}}` operands inside a gossip predicate: the parser must record
one Substitution for the B-side LHS so InstallFirmware can patch the
predicate instruction's bStart per Value, the predicate itself must
land on TopoHypercubePerPeer so the kernel runs it per peer, and the
body instructions inside the predicate must inherit that topology and
read their mask from the per-peer scratch slot.
*/
func TestCompileIndirectOperands(t *testing.T) {
	Convey("Given a gossip predicate with indirect operands", t, func() {
		source := `
program query {
  gossip(B) {
    (B.{{A.context[0,1]}} == {{A.context[1,1]}}) {
      write B.properties.reference <- A.properties.reference
      stage(B)
    }
  }
}
`

		result, err := Compile(source)
		So(err, ShouldBeNil)

		predicateWord := result.Words[0]
		bodyWord := result.Words[1]
		stageWord := result.Words[2]

		Convey("It should emit the predicate on TopoHypercubePerPeer", func() {
			topology := (predicateWord >> 55) & 3
			predicate := (predicateWord >> 57) & 1

			So(topology, ShouldEqual, uint64(TopoHypercubePerPeer))
			So(predicate, ShouldEqual, uint64(1))
		})

		Convey("It should record one substitution for the B-side LHS", func() {
			lhsSubs := 0
			for _, sub := range result.Substitutions {
				if sub.PC == 0 && sub.FieldShift == SubstAStartShift {
					lhsSubs++
					So(sub.Addr, ShouldEqual, uint64(contextStart))
				}
			}

			So(lhsSubs, ShouldEqual, 1)
		})

		Convey("It should point the threshold operand at A.context[1] without substitution", func() {
			bStart := (predicateWord >> 18) & 0x7F
			So(bStart, ShouldEqual, uint64(contextStart+1))
		})

		Convey("It should retarget the predicate result slot to the per-peer scratch word", func() {
			dstStart := (predicateWord >> 32) & 0x7F
			So(dstStart, ShouldEqual, uint64(PerPeerMaskWord))
		})

		Convey("It should emit the body write on TopoHypercubePerPeer with the per-peer mask", func() {
			topology := (bodyWord >> 55) & 3
			maskStart := (bodyWord >> 46) & 0x7F

			So(topology, ShouldEqual, uint64(TopoHypercubePerPeer))
			So(maskStart, ShouldEqual, uint64(PerPeerMaskWord))
		})

		Convey("It should emit stage(B) on TopoHypercubePerPeer", func() {
			topology := (stageWord >> 55) & 3
			stage := (stageWord >> 62) & 1

			So(topology, ShouldEqual, uint64(TopoHypercubePerPeer))
			So(stage, ShouldEqual, uint64(1))
		})
	})
}
