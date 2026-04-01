package cpu

/*
broadcastFillU64 replicates value into every element of dst.

Doubling copy turns one store into full coverage in ceil(log2(n)) memcpy
bursts, which exercises wide cache lines and avoids issuing n scalar stores
before AVX blocks run — the scalar fill was a serial bottleneck ahead of SIMD ALU.
*/
func broadcastFillU64(dst []uint64, value uint64) {

	if len(dst) == 0 {
		return
	}

	dst[0] = value

	for bp := 1; bp < len(dst); bp <<= 1 {
		copy(dst[bp:], dst[:bp])
	}
}
