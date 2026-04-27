package program

/*
Compiled is the packed resident sweep (at most 16 words for the stock ABI).
*/
type Compiled struct {
	Words []uint64
}
