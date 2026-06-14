package compiler

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func BenchmarkCompileGeometric(b *testing.B) {
	source := `
program geometric_probe {
  geometric compose
  geometric sandwich
  geometric reverse
}
`

	b.ReportAllocs()
	b.ResetTimer()

	for iteration := 0; iteration < b.N; iteration++ {
		_, _ = Compile(source)
	}
}

func TestCompile(t *testing.T) {
	Convey("Given a geometric resident program", t, func() {
		source := `
program geometric_probe {
  geometric compose
  geometric sandwich
  geometric reverse
}
`

		Convey("It should lower PGA slots as raw opcode words", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)
			So(result.Words[0], ShouldEqual, uint64(OpGeometricCompose))
			So(result.Words[1], ShouldEqual, uint64(OpGeometricSandwich))
			So(result.Words[2], ShouldEqual, uint64(OpGeometricReverse))
		})
	})

	Convey("Given strict reducer names", t, func() {
		cases := []string{
			`program bad { set A.properties.community <- argmin_nonzero(B.properties.community, B.properties.surprisal) }`,
			`program bad { set A.properties.labels <- mode_eq(B.properties.labels, B.properties.community, A.properties.community) }`,
			`program bad { set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature) }`,
			`program bad { set A.context[0,8] <- geo_centroid(B.context[0,8], B.properties.labels, A.properties.labels) }`,
			`program bad { set A.properties.labels <- geo_nearest(B.properties.labels, B.context[0,8], A.context[0,8]) }`,
			`program bad { set A.properties.labels <- run_zero(A.signals[0,8]) }`,
			`program bad { set A.properties.target <- run_one(A.signals[0,8]) }`,
			`program bad { set A.signals[0,8] <- align_zero(A.tokens[0,8], B.tokens[0,8]) }`,
			`program bad { set A.properties.labels <- any_zero(A.signals[0,8]) }`,
		}

		Convey("It should reject named reducers and legacy helper calls", func() {
			for _, source := range cases {
				_, err := Compile(source)
				So(err, ShouldNotBeNil)
			}
		})
	})

	Convey("Given generic scalar ALU calls", t, func() {
		source := `
program scalar_probe {
  set A.signals[0,1] <- shiftl(A.tokens[0,1], 3)
  set A.signals[1,1] <- shiftr(A.tokens[1,1], 1)
  set A.signals[2,1] <- rotl(A.tokens[2,1], 8)
  set A.signals[3,1] <- rotr(A.tokens[3,1], 8)
}
`

		Convey("It should lower shifts and rotates into the scalar sublane", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			ops := []uint64{ScalarShiftLeft, ScalarShiftRight, ScalarRotateLeft, ScalarRotateRight}
			for idx, op := range ops {
				word := result.Words[idx]
				So(word&0xF, ShouldEqual, op)
				So((word>>57)&1, ShouldEqual, uint64(1))
				So((word>>58)&7, ShouldEqual, uint64(PredScalar))
			}
		})
	})

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

  ; Sweep the community as in-band peer state.
  gossip(B) {
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

		Convey("It should encode emit body writes with the child target", func() {
			result, _ := Compile(source)

			foundChildTarget := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>53)&3 == 2 {
					foundChildTarget = true
				}
			}

			So(foundChildTarget, ShouldBeTrue)
		})

		Convey("It should encode peer writes on gossip topology", func() {
			result, _ := Compile(source)

			foundGossip := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>55)&3 == TopoHypercubePerPeer || (word>>55)&3 == TopoHypercube {
					foundGossip = true
				}
			}

			So(foundGossip, ShouldBeTrue)
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
			// two fresh per-IF mask words), 5 (DONE, used twice — DONE
			// shifted from 4 to 5 once SELECTED was inserted ahead of
			// it in the status enum).
			So(values[120], ShouldEqual, 1)
			So(values[5], ShouldEqual, 2)
			So(values[0], ShouldBeGreaterThanOrEqualTo, 1)
		})
	})
}

func TestCompileBPredicate(t *testing.T) {
	Convey("Given a query source that predicates on popped B state", t, func() {
		source := `
program query {
  gossip(B) {
    (B.properties.community == 0) {
      write B.properties.reference <- A.properties.reference
      set B.properties.status <- SELECTED
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
  gossip(B) {
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
  gossip(B) {
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
  gossip(B) {
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

		Convey("It should target the emitted child for the emit body", func() {
			result, err := Compile(source)
			So(err, ShouldBeNil)

			childTargets := 0
			for _, word := range result.Words {
				if word != 0 && (word>>53)&3 == 2 {
					childTargets++
				}
			}

			So(childTargets, ShouldBeGreaterThan, 0)
		})
	})
}

func TestCompileReducers(t *testing.T) {
	Convey("Given strict reducer source", t, func() {
		sources := []string{
			`program reducers { set A.properties.community <- argmin_nonzero(B.properties.community, B.properties.surprisal) }`,
			`program reducers { set A.properties.labels <- mode_eq(B.properties.labels, B.properties.community, A.properties.community) }`,
			`program reducers { set A.properties.program_id <- zipf_select(B.properties.program_id, B.properties.confidence, A.properties.temperature) }`,
		}

		Convey("It should reject public reducer calls", func() {
			for _, source := range sources {
				_, err := Compile(source)
				So(err, ShouldNotBeNil)
			}
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
  gossip(B) {
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
program scalar {
  set A.signals[0,1] <- shiftl(A.tokens[0,1], 3)
  set A.signals[1,1] <- shiftr(A.tokens[1,1], 1)
  set A.signals[2,1] <- rotl(A.tokens[2,1], 8)
  set A.signals[3,1] <- rotr(A.tokens[3,1], 8)
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
      set B.properties.status <- SELECTED
    }
  }
}
`

		result, err := Compile(source)
		So(err, ShouldBeNil)

		predicateWord := result.Words[0]
		bodyWord := result.Words[1]

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

		Convey("It should point the threshold operand at a patched constant slot", func() {
			bStart := (predicateWord >> 18) & 0x7F
			So(bStart, ShouldBeGreaterThanOrEqualTo, uint64(assetConstStart))

			foundConstantSub := false
			for _, sub := range result.Substitutions {
				if sub.Constant && sub.ConstantOffset == bStart {
					foundConstantSub = true
					So(sub.Addr, ShouldEqual, uint64(contextStart+1))
				}
			}

			So(foundConstantSub, ShouldBeTrue)
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

		Convey("It should emit status selection on TopoHypercubePerPeer", func() {
			statusWord := result.Words[2]
			topology := (statusWord >> 55) & 3
			target := (statusWord >> 53) & 3

			So(topology, ShouldEqual, uint64(TopoHypercubePerPeer))
			So(target, ShouldEqual, uint64(1))
		})
	})
}

const compileDirectWakeSource = `
program query {
  gossip(B) {
    (B.id == A.properties.reference) {
      set B.properties.status <- READY
    }
  }
}
`

const compileQueryWakeSource = `
program query {
  gossip(B) {
    (B.{{A.context[0,1]}} == {{A.context[1,1]}}) {
      write B.properties.reference <- A.properties.reference
      set B.properties.status <- SELECTED

      (B.id == A.properties.reference) {
        set B.properties.status <- READY
      }
    }
  }

  set A.properties.status <- DONE
}
`

func TestCompileQueryWake(t *testing.T) {
	Convey("Given the direct id wake predicate", t, func() {
		Convey("It should compile the direct peer id comparison", func() {
			result, err := Compile(compileDirectWakeSource)
			So(err, ShouldBeNil)

			predicateWord := result.Words[0]
			bodyWord := result.Words[1]

			So(predicateWord, ShouldNotEqual, uint64(0))
			So((predicateWord>>55)&3, ShouldEqual, uint64(TopoHypercubePerPeer))
			So((predicateWord>>57)&1, ShouldEqual, uint64(1))
			So((predicateWord>>61)&1, ShouldEqual, uint64(1))
			So((predicateWord>>4)&0x7F, ShouldEqual, uint64(idStart))
			So((predicateWord>>18)&0x7F, ShouldEqual, uint64(propertiesStart+11))

			So(bodyWord, ShouldNotEqual, uint64(0))
			So((bodyWord>>53)&3, ShouldEqual, uint64(1))
			So((bodyWord>>32)&0x7F, ShouldEqual, uint64(propertiesStart+5))

			So(len(result.Constants), ShouldBeGreaterThan, 0)
			So(len(result.Substitutions), ShouldEqual, 0)
		})
	})

	Convey("Given the query wake source", t, func() {
		Convey("It should compile the indirect selector and nested wake predicate", func() {
			result, err := Compile(compileQueryWakeSource)
			So(err, ShouldBeNil)

			predicateWord := result.Words[0]
			So(predicateWord, ShouldNotEqual, uint64(0))
			So((predicateWord>>55)&3, ShouldEqual, uint64(TopoHypercubePerPeer))
			So((predicateWord>>57)&1, ShouldEqual, uint64(1))
			So((predicateWord>>32)&0x7F, ShouldEqual, uint64(PerPeerMaskWord))

			thresholdStart := (predicateWord >> 18) & 0x7F
			So(thresholdStart, ShouldBeGreaterThanOrEqualTo, uint64(assetConstStart))

			foundOperandSub := false
			foundConstantSub := false
			for _, sub := range result.Substitutions {
				if sub.PC == 0 && sub.FieldShift == SubstAStartShift {
					foundOperandSub = true
					So(sub.Addr, ShouldEqual, uint64(contextStart))
				}

				if sub.Constant && sub.ConstantOffset == thresholdStart {
					foundConstantSub = true
					So(sub.Addr, ShouldEqual, uint64(contextStart+1))
				}
			}

			So(foundOperandSub, ShouldBeTrue)
			So(foundConstantSub, ShouldBeTrue)

			statusWritesToB := 0
			nestedWakePredicate := false
			for _, word := range result.Words {
				if word == 0 {
					continue
				}

				if (word>>53)&3 == 1 && (word>>32)&0x7F == uint64(propertiesStart+5) {
					statusWritesToB++
				}

				if (word>>57)&1 == 1 && (word>>4)&0x7F == uint64(idStart) && (word>>18)&0x7F == uint64(propertiesStart+11) {
					nestedWakePredicate = true
				}
			}

			So(statusWritesToB, ShouldBeGreaterThanOrEqualTo, 2)
			So(nestedWakePredicate, ShouldBeTrue)
			So(len(result.Constants), ShouldBeGreaterThan, 0)
		})
	})
}

func BenchmarkCompileQueryWake(b *testing.B) {
	b.ReportAllocs()

	for b.Loop() {
		if _, err := Compile(compileQueryWakeSource); err != nil {
			b.Fatal(err)
		}
	}
}
