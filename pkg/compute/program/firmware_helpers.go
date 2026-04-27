package program

import (
	"fmt"
	"strings"
	"unicode"
)

func extractBraceBlock(src string) (inner string, err error) {
	src = strings.TrimSpace(src)
	if !strings.HasPrefix(src, "{") {
		return "", fmt.Errorf("firmware: expected {")
	}
	depth := 0
	for idx := 0; idx < len(src); idx++ {
		switch src[idx] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return strings.TrimSpace(src[1:idx]), nil
			}
		}
	}
	return "", fmt.Errorf("firmware: unbalanced braces")
}

func splitTopLevel(body string) []string {
	body = strings.TrimSpace(body)
	var out []string
	depth := 0
	start := 0
	for idx := 0; idx < len(body); idx++ {
		switch body[idx] {
		case '{':
			depth++
		case '}':
			depth--
		case ';', '\n':
			if depth == 0 {
				part := strings.TrimSpace(body[start:idx])
				if part != "" {
					out = append(out, part)
				}
				start = idx + 1
			}
		}
	}
	part := strings.TrimSpace(body[start:])
	if part != "" {
		out = append(out, part)
	}
	return out
}

func parseWhen(stmt string) (cond string, bracePart string, err error) {
	stmt = strings.TrimSpace(stmt)
	if !strings.HasPrefix(stmt, "when ") {
		return "", "", fmt.Errorf("firmware: when missing prefix")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(stmt, "when "))
	idx := strings.IndexByte(rest, '{')
	if idx < 0 {
		return "", "", fmt.Errorf("firmware: when missing {")
	}
	cond = strings.TrimSpace(rest[:idx])
	bracePart = strings.TrimSpace(rest[idx:])
	return cond, bracePart, nil
}

func splitArrow(s string) (lhs, rhs string, ok bool) {
	idx := strings.Index(s, "<-")
	if idx < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:idx]), strings.TrimSpace(s[idx+2:]), true
}

func stripLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = line[:idx]
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

func parseHammingLine(whereLine string) (aPath, bPath string, th uint64, err error) {
	whereLine = strings.TrimSpace(whereLine)
	if !strings.HasPrefix(whereLine, "hamming") {
		return "", "", 0, fmt.Errorf("firmware: expected hamming(…)")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(whereLine, "hamming"))
	arg, tail, err := parseCallArgs(rest)
	if err != nil {
		return "", "", 0, err
	}
	pair := splitArgs(arg)
	if len(pair) != 2 {
		return "", "", 0, fmt.Errorf("firmware: hamming needs two comma-separated refs")
	}
	th, err = parseCmpThreshold(tail)
	if err != nil {
		return "", "", 0, err
	}
	return strings.TrimSpace(pair[0]), strings.TrimSpace(pair[1]), th, nil
}
