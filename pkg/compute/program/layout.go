package program

type RegionExtent struct {
	Start int
	Words int
}

type Layout struct {
	Regions    map[string]RegionExtent
	Properties map[string]int
	Opcodes    map[string]uint64
}

type Compiled struct {
	Words []uint64
}
