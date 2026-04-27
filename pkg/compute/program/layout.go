package program

/*
RegionExtent is one ABI region: absolute start word, word count, semantic bit width.
*/
type RegionExtent struct {
	Start int
	Words int
	Bits  uint64
}

/*
Layout resolves symbolic paths from config (regions, properties, opcode nibbles).
*/
type Layout struct {
	Regions    map[string]RegionExtent
	Properties map[string]int
	Opcodes    map[string]uint64
	/*
		StatusValue maps names like DONE / PENDING to the uint64 stored in the
		status property word (from config value.status order).
	*/
	StatusValue map[string]uint64
}

func MergeOpcodes(lay Layout) map[string]uint64 {
	out := make(map[string]uint64, len(DefaultTruthOpcodes)+len(lay.Opcodes))
	for key, value := range DefaultTruthOpcodes {
		out[key] = value
	}
	for key, value := range lay.Opcodes {
		out[key] = value
	}
	return out
}
