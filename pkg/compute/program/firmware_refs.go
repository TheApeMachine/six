package program

import (
	"fmt"
	"strconv"
	"strings"
)

type actorKind byte

const (
	actorInvalid actorKind = iota
	actorA
	actorB
	actorChild
)

/*
resolvedRef is an absolute word span on the Value frame (same indices for child emit).
*/
type resolvedRef struct {
	actor    actorKind
	start    int
	span     int
	indirect bool
}

func parseActorPrefix(path string) (actor actorKind, rest string, err error) {
	path = strings.TrimSpace(path)
	if strings.HasPrefix(path, "A.") {
		return actorA, strings.TrimPrefix(path, "A."), nil
	}
	if strings.HasPrefix(path, "B.") {
		return actorB, strings.TrimPrefix(path, "B."), nil
	}
	if strings.HasPrefix(path, "child.") {
		return actorChild, strings.TrimPrefix(path, "child."), nil
	}
	return actorInvalid, "", fmt.Errorf("firmware: missing actor prefix in %q", path)
}

func resolveRef(lay Layout, path string) (resolvedRef, error) {
	actor, rest, err := parseActorPrefix(path)
	if err != nil {
		return resolvedRef{}, err
	}
	indirect := false
	if strings.HasPrefix(rest, "*") {
		indirect = true
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "*"))
	}

	ref, err := resolveBare(lay, rest)
	if err != nil {
		return resolvedRef{}, err
	}
	ref.actor = actor
	ref.indirect = indirect
	return ref, nil
}

func resolveBare(lay Layout, token string) (resolvedRef, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return resolvedRef{}, fmt.Errorf("firmware: empty ref")
	}

	if idx := strings.IndexByte(token, '['); idx >= 0 && strings.HasSuffix(token, "]") {
		name := strings.ToLower(token[:idx])
		body := token[idx+1 : len(token)-1]
		parts := strings.Split(body, ",")
		relStart, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			return resolvedRef{}, fmt.Errorf("firmware: bad index in %q", token)
		}
		span := 1
		if len(parts) == 2 {
			span, err = strconv.Atoi(strings.TrimSpace(parts[1]))
			if err != nil {
				return resolvedRef{}, fmt.Errorf("firmware: bad span in %q", token)
			}
		}
		reg, ok := lay.Regions[name]
		if !ok {
			return resolvedRef{}, fmt.Errorf("firmware: unknown region %q", name)
		}
		return resolvedRef{start: reg.Start + relStart, span: span}, nil
	}

	if dot := strings.IndexByte(token, '.'); dot >= 0 {
		regionName := strings.ToLower(strings.TrimSpace(token[:dot]))
		propName := strings.ToLower(strings.TrimSpace(token[dot+1:]))
		if regionName != "properties" {
			return resolvedRef{}, fmt.Errorf("firmware: dotted path must be properties.* got %q", token)
		}
		off, ok := lay.Properties[propName]
		if !ok {
			return resolvedRef{}, fmt.Errorf("firmware: unknown property %q", propName)
		}
		reg, ok := lay.Regions["properties"]
		if !ok {
			return resolvedRef{}, fmt.Errorf("firmware: layout missing properties region")
		}
		return resolvedRef{start: reg.Start + off, span: 1}, nil
	}

	name := strings.ToLower(token)
	if reg, ok := lay.Regions[name]; ok {
		return resolvedRef{start: reg.Start, span: reg.Words}, nil
	}
	if off, ok := lay.Properties[name]; ok {
		reg, ok := lay.Regions["properties"]
		if !ok {
			return resolvedRef{}, fmt.Errorf("firmware: layout missing properties region")
		}
		return resolvedRef{start: reg.Start + off, span: 1}, nil
	}

	return resolvedRef{}, fmt.Errorf("firmware: unknown ref %q", token)
}
