package programmer

import (
	"sync"
)

var programCache sync.Map

type cachedProgram struct {
	frames []Frame
	cont   *Continuation
}

func getCachedProgram(nameOrSource string) (cachedProgram, bool) {
	if val, ok := programCache.Load(nameOrSource); ok {
		return val.(cachedProgram), true
	}
	return cachedProgram{}, false
}

func setCachedProgram(nameOrSource string, cp cachedProgram) {
	programCache.Store(nameOrSource, cp)
}
