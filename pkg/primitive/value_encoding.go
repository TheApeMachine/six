package primitive

/*
trigramMixByte folds three payload bytes into a single bind key so n-gram
superposition stays in-band without multi-megabyte trigram tables.
*/
func trigramMixByte(b0, b1, b2 byte) byte {

	return byte((int(b0)*131 + int(b1)*17) ^ int(b2))
}
