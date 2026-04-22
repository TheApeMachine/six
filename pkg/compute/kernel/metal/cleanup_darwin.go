//go:build darwin && cgo

package metal

/*
#cgo CXXFLAGS: -x objective-c++
#cgo LDFLAGS: -framework Metal -framework Foundation
#include "metal.h"
#include <stdlib.h>
void cleanup_metal_pools(void);
static inline int six_cleanup_metal_pools(void) {
	cleanup_metal_pools();

	return 0;
}
*/
import "C"

func cleanupMetalPools() {
	_ = C.six_cleanup_metal_pools()
}

