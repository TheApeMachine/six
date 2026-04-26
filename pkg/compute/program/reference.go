package program

import (
	"fmt"
	"strconv"
	"strings"
)

func isBareFeedRef(raw string) bool {
	for idx, char := range raw {
		if char >= 'a' && char <= 'z' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= '0' && char <= '9' && idx > 0 {
			continue
		}
		if char == '_' || char == '.' {
			continue
		}

		return false
	}

	return raw != ""
}

func rotateFeedRef(raw string, amount int) (string, error) {
	open := strings.IndexByte(raw, '[')
	if open < 0 || !strings.HasSuffix(raw, "]") {
		return "", fmt.Errorf("rotation requires indexed region, got %q", raw)
	}

	name := raw[:open]
	body := raw[open+1 : len(raw)-1]
	parts := strings.Split(body, ",")
	if len(parts) == 0 || len(parts) > 2 {
		return "", fmt.Errorf("invalid indexed region %q", raw)
	}

	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return "", err
	}

	span := 1
	if len(parts) == 2 {
		span, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return "", err
		}
	}
	if span <= 0 {
		return "", fmt.Errorf("invalid span in %q", raw)
	}

	rotated := (start + amount) % span
	if rotated < 0 {
		rotated += span
	}

	return fmt.Sprintf("%s[%d,%d]", name, rotated, span-rotated), nil
}

func isKnownFeedRegion(raw string) bool {
	switch strings.ToLower(raw) {
	case "tokens", "program", "signals", "context", "gradient", "properties", "asset", "prev", "next", "id", "affinity":
		return true
	default:
		return false
	}
}

func parseFeedOperand(atom feedAtom, lay Layout) (start, span int, indirect uint64, err error) {
	if !atom.imm {
		return parseRef(atom.ref, lay)
	}

	switch atom.ref {
	case "clear":
		return 0, 1, 0, nil
	case "done":
		return 4, 1, 0, nil
	default:
		imm, err := strconv.ParseUint(atom.ref, 10, 14)
		if err != nil {
			return 0, 0, 0, err
		}

		return int(imm & 0x7F), int((imm>>7)&0x7F) + 1, 0, nil
	}
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}

	for _, char := range s {
		if char < '0' || char > '9' {
			return false
		}
	}

	return true
}

func parseRef(token string, lay Layout) (start, span int, indirect uint64, err error) {
	if strings.HasPrefix(token, "*") {
		indirect = 1
		token = token[1:]
	}

	if isNumeric(token) {
		start, _ = strconv.Atoi(token)
		span = 1

		return start, span, indirect, nil
	}

	if parts := strings.Split(token, ".."); len(parts) == 2 {
		if isNumeric(parts[0]) && isNumeric(parts[1]) {
			rangeStart, _ := strconv.Atoi(parts[0])
			rangeEnd, _ := strconv.Atoi(parts[1])
			if rangeEnd < rangeStart {
				return 0, 0, 0, fmt.Errorf("invalid range %s", token)
			}

			return rangeStart, rangeEnd - rangeStart, indirect, nil
		}
	}

	if idx := strings.IndexByte(token, '.'); idx >= 0 {
		regionName := strings.ToLower(token[:idx])
		propName := strings.ToLower(token[idx+1:])

		region, ok := lay.Regions[regionName]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown region %q", regionName)
		}

		propOffset, ok := lay.Properties[propName]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown property %q in region %q", propName, regionName)
		}

		return region.Start + propOffset, 1, indirect, nil
	}

	open := strings.IndexByte(token, '[')
	if open >= 0 && strings.HasSuffix(token, "]") {
		name := strings.ToLower(token[:open])
		region, ok := lay.Regions[name]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown region %q", name)
		}

		body := token[open+1 : len(token)-1]
		parts := strings.Split(body, ",")
		relStart, _ := strconv.Atoi(strings.TrimSpace(parts[0]))
		span = 1
		if len(parts) == 2 {
			span, _ = strconv.Atoi(strings.TrimSpace(parts[1]))
		}

		return region.Start + relStart, span, indirect, nil
	}

	name := strings.ToLower(token)
	if region, ok := lay.Regions[name]; ok {
		return region.Start, region.Words, indirect, nil
	}
	if propOffset, ok := lay.Properties[name]; ok {
		region, ok := lay.Regions["properties"]
		if !ok {
			return 0, 0, 0, fmt.Errorf("unknown properties region")
		}

		return region.Start + propOffset, 1, indirect, nil
	}

	return 0, 0, 0, fmt.Errorf("unknown region alias %q", token)
}
