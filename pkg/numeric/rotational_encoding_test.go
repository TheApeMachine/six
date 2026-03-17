package numeric

import (
	"testing"

	gc "github.com/smartystreets/goconvey/convey"
)

/*
buildDlogTable computes the discrete logarithm table for GF(257)
with primitive root 3. Returns:
  - exp: exponent k → 3^k mod 257
  - dlog: element g → k such that 3^k = g
*/
func buildDlogTable() (exp [256]uint16, dlog [257]uint16) {
	var power uint16 = 1

	for k := 0; k < 256; k++ {
		exp[k] = power
		dlog[power] = uint16(k)
		power = (power * 3) % 257
	}

	return
}

/*
primeDlogs computes the discrete log of each of the first 257 primes
(mod 257). Primes whose residue is 0 (i.e., prime 257 itself) are
flagged with valid=false.
*/
func primeDlogs(dlog [257]uint16) (dlogs [257]uint16, valid [257]bool) {
	for i := 0; i < 257 && i < len(Primes); i++ {
		residue := uint16(Primes[i]) % 257

		if residue == 0 {
			valid[i] = false
			continue
		}

		dlogs[i] = dlog[residue]
		valid[i] = true
	}

	return
}

/*
find5Primes finds 5 prime indices (from the first 257 primes) whose
discrete logs sum to target mod 256. Returns the 5 indices and true
if found, or false if no combination exists.
Uses a constructive approach: fix 4 primes, compute the needed 5th.
*/
func find5Primes(
	pDlogs [257]uint16, pValid [257]bool, target uint16,
) ([5]int, bool) {
	n := 257
	if len(Primes) < n {
		n = len(Primes)
	}

	for primeIdxA := 0; primeIdxA < n; primeIdxA++ {
		if !pValid[primeIdxA] {
			continue
		}

		for primeIdxB := primeIdxA + 1; primeIdxB < n; primeIdxB++ {
			if !pValid[primeIdxB] {
				continue
			}

			for primeIdxC := primeIdxB + 1; primeIdxC < n; primeIdxC++ {
				if !pValid[primeIdxC] {
					continue
				}

				for primeIdxD := primeIdxC + 1; primeIdxD < n; primeIdxD++ {
					if !pValid[primeIdxD] {
						continue
					}

					sum4 := (pDlogs[primeIdxA] + pDlogs[primeIdxB] + pDlogs[primeIdxC] + pDlogs[primeIdxD]) % 256
					need := (target - sum4 + 256) % 256

					for primeIdxE := primeIdxD + 1; primeIdxE < n; primeIdxE++ {
						if !pValid[primeIdxE] {
							continue
						}

						if pDlogs[primeIdxE] == need {
							return [5]int{primeIdxA, primeIdxB, primeIdxC, primeIdxD, primeIdxE}, true
						}
					}
				}
			}
		}
	}

	return [5]int{}, false
}

func TestRotationalEncoding(t *testing.T) {
	gc.Convey("Given GF(257) with primitive root 3", t, func() {
		exp, dlog := buildDlogTable()

		gc.Convey("3^256 mod 257 should equal 1 (full cycle)", func() {
			gc.So(exp[0], gc.ShouldEqual, 1)

			product := uint16(1)
			for range 256 {
				product = (product * 3) % 257
			}

			gc.So(product, gc.ShouldEqual, 1)
		})

		gc.Convey("Every byte value 1-255 should have a unique discrete log", func() {
			seen := make(map[uint16]bool)

			for b := 1; b <= 255; b++ {
				k := dlog[uint16(b)]
				gc.So(seen[k], gc.ShouldBeFalse)
				seen[k] = true
				gc.So(exp[k], gc.ShouldEqual, uint16(b))
			}
		})

		gc.Convey("A byte transition should be computable as a rotation", func() {
			a := uint16('T')
			b := uint16('h')

			d := (dlog[b] - dlog[a] + 256) % 256
			result := (a * exp[d]) % 257

			gc.So(result, gc.ShouldEqual, b)
		})

		gc.Convey("A full sequence should roundtrip through rotations", func() {
			seq := []byte("The cat sat on the mat")

			rotations := make([]uint16, len(seq)-1)

			for i := 0; i < len(seq)-1; i++ {
				curr := uint16(seq[i])
				next := uint16(seq[i+1])
				rotations[i] = (dlog[next] - dlog[curr] + 256) % 256
			}

			decoded := make([]byte, len(seq))
			decoded[0] = seq[0]

			for i, d := range rotations {
				curr := uint16(decoded[i])
				next := (curr * exp[d]) % 257
				decoded[i+1] = byte(next)
			}

			gc.So(string(decoded), gc.ShouldEqual, string(seq))
		})

		gc.Convey("Rotation exponents should be recoverable from 5 prime dlogs", func() {
			pDlogs, pValid := primeDlogs(dlog)

			seq := []byte("Hi")
			curr := uint16(seq[0])
			next := uint16(seq[1])
			target := (dlog[next] - dlog[curr] + 256) % 256

			indices, found := find5Primes(pDlogs, pValid, target)
			gc.So(found, gc.ShouldBeTrue)

			recoveredD := uint16(0)
			for _, idx := range indices {
				recoveredD = (recoveredD + pDlogs[idx]) % 256
			}

			gc.So(recoveredD, gc.ShouldEqual, target)

			recoveredNext := (curr * exp[recoveredD]) % 257
			gc.So(byte(recoveredNext), gc.ShouldEqual, seq[1])
		})
	})
}

func BenchmarkFind5Primes(b *testing.B) {
	_, dlog := buildDlogTable()
	pDlogs, pValid := primeDlogs(dlog)

	b.ResetTimer()

	for range b.N {
		find5Primes(pDlogs, pValid, 42)
	}
}
