// Copyright (c) 2020 Robert Clausecker <fuz@fuz.su>

package pospop

// 64-bit full adder (CSA building block).
func csa64(a, b, c uint64) (c_out, s uint64) {
	s_ab := a ^ b
	c_ab := a & b

	s = s_ab ^ c
	c_out = c_ab | s_ab&c

	return
}

// count64generic implements positional popcount using the same CSA15 kernel as the SIMD paths.
func count64generic(counts *[64]int, buf []uint64) {
	var index int

	for index = 0; index < len(buf)-14; index += 15 {
		b0, a0 := csa64(buf[index+0], buf[index+1], buf[index+2])
		b1, a1 := csa64(buf[index+3], buf[index+4], buf[index+5])
		b2, a2 := csa64(a0, a1, buf[index+6])
		c0, b3 := csa64(b0, b1, b2)
		b4, a3 := csa64(a2, buf[index+7], buf[index+8])
		b5, a4 := csa64(a3, buf[index+9], buf[index+10])
		c1, b6 := csa64(b3, b4, b5)
		b7, a5 := csa64(a4, buf[index+11], buf[index+12])
		b8, a := csa64(a5, buf[index+13], buf[index+14])
		c2, b := csa64(b6, b7, b8)
		d, c := csa64(c0, c1, c2)

		ba0 := a&0x5555555555555555 | b<<1&0xaaaaaaaaaaaaaaaa
		ba1 := a>>1&0x5555555555555555 | b&0xaaaaaaaaaaaaaaaa
		dc0 := c&0x5555555555555555 | d<<1&0xaaaaaaaaaaaaaaaa
		dc1 := c>>1&0x5555555555555555 | d&0xaaaaaaaaaaaaaaaa

		dcba0 := ba0&0x3333333333333333 | dc0<<2&0xcccccccccccccccc
		dcba1 := ba0>>2&0x3333333333333333 | dc0&0xcccccccccccccccc
		dcba2 := ba1&0x3333333333333333 | dc1<<2&0xcccccccccccccccc
		dcba3 := ba1>>2&0x3333333333333333 | dc1&0xcccccccccccccccc

		dcba0l := uint(uint32(dcba0))
		dcba0h := uint(dcba0 >> 32)
		dcba1l := uint(uint32(dcba1))
		dcba1h := uint(dcba1 >> 32)
		dcba2l := uint(uint32(dcba2))
		dcba2h := uint(dcba2 >> 32)
		dcba3l := uint(uint32(dcba3))
		dcba3h := uint(dcba3 >> 32)

		counts[0] += int(dcba0l & 0x0f)
		counts[1] += int(dcba2l & 0x0f)
		counts[2] += int(dcba1l & 0x0f)
		counts[3] += int(dcba3l & 0x0f)
		counts[4] += int(dcba0l >> 4 & 0x0f)
		counts[5] += int(dcba2l >> 4 & 0x0f)
		counts[6] += int(dcba1l >> 4 & 0x0f)
		counts[7] += int(dcba3l >> 4 & 0x0f)
		counts[8] += int(dcba0l >> 8 & 0x0f)
		counts[9] += int(dcba2l >> 8 & 0x0f)
		counts[10] += int(dcba1l >> 8 & 0x0f)
		counts[11] += int(dcba3l >> 8 & 0x0f)
		counts[12] += int(dcba0l >> 12 & 0x0f)
		counts[13] += int(dcba2l >> 12 & 0x0f)
		counts[14] += int(dcba1l >> 12 & 0x0f)
		counts[15] += int(dcba3l >> 12 & 0x0f)
		counts[16] += int(dcba0l >> 16 & 0x0f)
		counts[17] += int(dcba2l >> 16 & 0x0f)
		counts[18] += int(dcba1l >> 16 & 0x0f)
		counts[19] += int(dcba3l >> 16 & 0x0f)
		counts[20] += int(dcba0l >> 20 & 0x0f)
		counts[21] += int(dcba2l >> 20 & 0x0f)
		counts[22] += int(dcba1l >> 20 & 0x0f)
		counts[23] += int(dcba3l >> 20 & 0x0f)
		counts[24] += int(dcba0l >> 24 & 0x0f)
		counts[25] += int(dcba2l >> 24 & 0x0f)
		counts[26] += int(dcba1l >> 24 & 0x0f)
		counts[27] += int(dcba3l >> 24 & 0x0f)
		counts[28] += int(dcba0l >> 28)
		counts[29] += int(dcba2l >> 28)
		counts[30] += int(dcba1l >> 28)
		counts[31] += int(dcba3l >> 28)

		counts[32] += int(dcba0h & 0x0f)
		counts[33] += int(dcba2h & 0x0f)
		counts[34] += int(dcba1h & 0x0f)
		counts[35] += int(dcba3h & 0x0f)
		counts[36] += int(dcba0h >> 4 & 0x0f)
		counts[37] += int(dcba2h >> 4 & 0x0f)
		counts[38] += int(dcba1h >> 4 & 0x0f)
		counts[39] += int(dcba3h >> 4 & 0x0f)
		counts[40] += int(dcba0h >> 8 & 0x0f)
		counts[41] += int(dcba2h >> 8 & 0x0f)
		counts[42] += int(dcba1h >> 8 & 0x0f)
		counts[43] += int(dcba3h >> 8 & 0x0f)
		counts[44] += int(dcba0h >> 12 & 0x0f)
		counts[45] += int(dcba2h >> 12 & 0x0f)
		counts[46] += int(dcba1h >> 12 & 0x0f)
		counts[47] += int(dcba3h >> 12 & 0x0f)
		counts[48] += int(dcba0h >> 16 & 0x0f)
		counts[49] += int(dcba2h >> 16 & 0x0f)
		counts[50] += int(dcba1h >> 16 & 0x0f)
		counts[51] += int(dcba3h >> 16 & 0x0f)
		counts[52] += int(dcba0h >> 20 & 0x0f)
		counts[53] += int(dcba2h >> 20 & 0x0f)
		counts[54] += int(dcba1h >> 20 & 0x0f)
		counts[55] += int(dcba3h >> 20 & 0x0f)
		counts[56] += int(dcba0h >> 24 & 0x0f)
		counts[57] += int(dcba2h >> 24 & 0x0f)
		counts[58] += int(dcba1h >> 24 & 0x0f)
		counts[59] += int(dcba3h >> 24 & 0x0f)
		counts[60] += int(dcba0h >> 28)
		counts[61] += int(dcba2h >> 28)
		counts[62] += int(dcba1h >> 28)
		counts[63] += int(dcba3h >> 28)
	}

	for ; index < len(buf); index++ {
		for bit := 0; bit < 64; bit++ {
			counts[bit] += int(buf[index] >> bit & 1)
		}
	}
}
