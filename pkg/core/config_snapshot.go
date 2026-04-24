package core

import "testing"

func deepCopyConfigSnapshot(src *Config) Config {
	if src == nil {
		return Config{}
	}

	dst := *src

	// StepwiseFirmwareSource is a fixed [FirmwareTypePrompt+1]string array; the
	// struct copy already duplicates array storage (string headers only).

	return dst
}

/*
PreserveGlobalConfig snapshots the process-wide Cfg and restores it on tb
cleanup. Tests that mutate Value region layout (kernel packages) should
call this first so parallel packages do not fight over the singleton when
go test -p N executes multiple packages at once — within a single suite
it also makes ordering explicit instead of relying on init races.
*/
func PreserveGlobalConfig(tb testing.TB) {
	tb.Helper()

	cfg := Cfg

	if cfg == nil {
		return
	}

	snap := deepCopyConfigSnapshot(cfg)

	tb.Cleanup(func() {
		current := Cfg

		if current == nil {
			return
		}

		*current = deepCopyConfigSnapshot(&snap)
	})
}
