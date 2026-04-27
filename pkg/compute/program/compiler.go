package program

import (
	"context"
	"fmt"
	"strings"
)

/*
Compile lowers authoring source to resident words. Only the firmware surface

	program <name> { ... }

is supported; there is no legacy feed parser.
*/
func Compile(ctx context.Context, source string, lay Layout) (Compiled, error) {
	trim := strings.TrimSpace(source)
	if !strings.HasPrefix(trim, "program ") {
		return Compiled{}, fmt.Errorf("program: only firmware syntax is supported (expected program … { … }), got %q", firstLine(trim))
	}

	words, err := compileFirmwareSource(ctx, trim, lay)
	if err != nil {
		return Compiled{}, err
	}

	return Compiled{Words: words}, nil
}

func firstLine(source string) string {
	if idx := strings.IndexByte(source, '\n'); idx >= 0 {
		return source[:idx] + " …"
	}
	if len(source) > 80 {
		return source[:80] + " …"
	}
	return source
}
