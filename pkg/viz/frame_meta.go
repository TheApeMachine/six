package viz

import (
	"fmt"
	"strings"

	"github.com/theapemachine/six/pkg/compute/kernel"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
AffinityHexFromFrame flattens the five affinity-region words (257 bits in five
uint64 lanes) into a single lower-case hex string for viz meta and UI.
*/
func AffinityHexFromFrame(v *primitive.Value) string {
	if v == nil {
		return ""
	}

	var out strings.Builder

	out.Grow(5 * 16)

	for wi := range 5 {
		fmt.Fprintf(&out, "%016x", (*v)[kernel.AffinityStartWord+wi])
	}

	return out.String()
}
