package projector

import "strings"

// LaTeXEscape escapes the characters that have special meaning in LaTeX so
// that arbitrary strings can be safely embedded in generated .tex files.
//
// Escaping rules (in order to avoid double-escaping):
//   - \  → \textbackslash{}   (must go first)
//   - {  → \{
//   - }  → \}
//   - %  → \%   (comment character — the most common silent bug)
//   - $  → \$
//   - &  → \&
//   - #  → \#
//   - _  → \_
//   - ^  → \^{}
//   - ~  → \textasciitilde{}
func LaTeXEscape(s string) string {
	// Backslash first, so we don't double-escape later replacements.
	s = strings.ReplaceAll(s, `\`, `\textbackslash{}`)
	s = strings.ReplaceAll(s, `{`, `\{`)
	s = strings.ReplaceAll(s, `}`, `\}`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `$`, `\$`)
	s = strings.ReplaceAll(s, `&`, `\&`)
	s = strings.ReplaceAll(s, `#`, `\#`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	s = strings.ReplaceAll(s, `^`, `\^{}`)
	s = strings.ReplaceAll(s, `~`, `\textasciitilde{}`)
	return s
}
