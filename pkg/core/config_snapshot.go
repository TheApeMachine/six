package core

import "testing"

/*
PreserveGlobalConfig snapshots the process-wide Cfg and restores it on tb
cleanup. Tests that mutate Value region layout (kernel packages) should
call this first so parallel packages do not fight over the singleton when
go test -p N executes multiple packages at once — within a single suite
it also makes ordering explicit instead of relying on init races.
*/
func PreserveGlobalConfig(tb testing.TB) {
	tb.Helper()

	if Cfg == nil {
		return
	}

	snap := *Cfg

	tb.Cleanup(func() {
		if Cfg == nil {
			return
		}

		*Cfg = snap
	})
}
