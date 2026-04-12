package programmer

import (
	"sync"

	"github.com/hashicorp/golang-lru/v2/simplelru"
)

/*
ProgramCacheMaxEntries caps how many distinct compiled programs stay resident; least-recently-used
entries are evicted under mutex protection.
*/
const ProgramCacheMaxEntries = 256

var (
	programCacheMu sync.Mutex
	programCache   *simplelru.LRU[string, cachedProgram]
)

func init() {
	next, err := simplelru.NewLRU[string, cachedProgram](ProgramCacheMaxEntries, nil)
	if err != nil {
		panic(err)
	}

	programCache = next
}

/*
SetProgramCacheMaxEntries replaces the LRU with a new empty cache of the given capacity.
Use for tests or tuning; max must be positive.
*/
func SetProgramCacheMaxEntries(max int) {
	if max <= 0 {
		return
	}

	next, err := simplelru.NewLRU[string, cachedProgram](max, nil)
	if err != nil {
		return
	}

	programCacheMu.Lock()
	programCache = next
	programCacheMu.Unlock()
}

type cachedProgram struct {
	frames []Frame
	cont   *Continuation
}

func getCachedProgram(nameOrSource string) (cachedProgram, bool) {
	programCacheMu.Lock()
	defer programCacheMu.Unlock()

	val, ok := programCache.Get(nameOrSource)
	if !ok {
		return cachedProgram{}, false
	}

	return val, true
}

func setCachedProgram(nameOrSource string, cp cachedProgram) {
	programCacheMu.Lock()
	defer programCacheMu.Unlock()

	programCache.Add(nameOrSource, cp)
}
