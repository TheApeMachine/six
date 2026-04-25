package pool

import _ "unsafe"

import "github.com/theapemachine/six/pkg/pool/constants"

const (
	cacheLinePadSize          = constants.CacheLinePadSize
	uint64SubtractionConstant = ^uint64(0)
)

// Semacquire waits until *s > 0 and then atomically decrements it.
//
//go:linkname runtime_Semacquire sync.runtime_Semacquire
func runtime_Semacquire(s *uint32)

// Semrelease atomically increments *s and notifies a waiting goroutine.
//
//go:linkname runtime_Semrelease sync.runtime_Semrelease
func runtime_Semrelease(s *uint32, handoff bool, skipframes int)
