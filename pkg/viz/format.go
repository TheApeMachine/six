package viz

import "fmt"

/*
FormatValueIDHex renders a Value’s 64-bit id word as fixed-width lower-case
hex so WebSocket meta keys match the on-frame word layout (no leading-zero
truncation from strconv/FormatUint).
*/
func FormatValueIDHex(id uint64) string {
	return fmt.Sprintf("%016x", id)
}
