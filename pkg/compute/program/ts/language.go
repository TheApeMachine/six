package ts

/*
#cgo CFLAGS: -std=c11 -I${SRCDIR}/../../../../grammar/tree-sitter-six/src
#include "tree_sitter/parser.h"

const TSLanguage *tree_sitter_six(void);
*/
import "C"

import (
	"context"
	"errors"
	"unsafe"

	sitter "github.com/alexaandru/go-tree-sitter-bare"
)

/*
ErrIncompatibleLanguage is returned when the generated parser’s ABI does not match go-tree-sitter-bare.
*/
var ErrIncompatibleLanguage = errors.New("compute/program/ts: tree-sitter language ABI mismatch")

/*
Language returns the Tree-sitter language descriptor for Six feed syntax.
*/
func Language() *sitter.Language {
	return sitter.NewLanguage(unsafe.Pointer(C.tree_sitter_six()))
}

/*
Parse returns the full syntax tree for feed source bytes.
*/
func Parse(ctx context.Context, source []byte) (*sitter.Tree, error) {
	parser := sitter.NewParser()
	if ok := parser.SetLanguage(Language()); !ok {
		return nil, ErrIncompatibleLanguage
	}

	return parser.ParseString(ctx, nil, source)
}
