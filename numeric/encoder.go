package numeric

import (
	"math"
	"math/cmplx"
)

/*
EncodeText takes a raw byte sequence and generates a 512-dimension PhaseDial.
This serves as a holographic vector embedding directly from the byte stream, mapping
symbol values to positional phase gradients. It uses the `NBasis` prime array.
*/
func EncodeText(text string) PhaseDial {
	dial := make(PhaseDial, NBasis)
	bytes := []byte(text)

	if len(bytes) == 0 {
		return dial
	}

	for k := 0; k < NBasis; k++ {
		var sum complex128
		omega := float64(prime.Basis[k])
		
		for t, b := range bytes {
			// Find prime corresponding to symbol (byte value) safely
			symbolPrime := float64(prime.Basis[int(b)%NSymbols])
			
			// Map structural position and symbol identity into a rotating phase.
			// Scaling down by an arbitrary phase constant (0.1) keeps rotation tight.
			phase := (omega * float64(t+1) * 0.1) + (symbolPrime * 0.1)
			
			// The accumulation across time gives the interference state
			sum += cmplx.Rect(1.0, phase)
		}
		
		// Each dimensional well captures the full string state across its frequency
		dial[k] = sum
	}

	return normalizePhaseDial(dial)
}

func normalizePhaseDial(dial PhaseDial) PhaseDial {
	var norm float64
	for _, val := range dial {
		r, i := real(val), imag(val)
		norm += r*r + i*i
	}
	
	if norm == 0 {
		return dial
	}
	
	norm = math.Sqrt(norm)
	
	for i := range dial {
		dial[i] = complex(real(dial[i])/norm, imag(dial[i])/norm)
	}
	
	return dial
}
