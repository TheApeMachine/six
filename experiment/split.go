package experiment

// BytePrefixFraction splits s by raw bytes so len(prefix)/len(full) ≈ prefixFraction.
// holdout is empty when splitting would not leave both sides non-empty.
func BytePrefixFraction(s string, prefixFraction float64) (prefix, holdout string) {
	if prefixFraction <= 0 || prefixFraction >= 1 {
		return s, ""
	}
	b := []byte(s)
	n := len(b)
	if n < 2 {
		return s, ""
	}
	cut := int(float64(n) * prefixFraction)
	if cut < 1 {
		cut = 1
	}
	if cut >= n {
		cut = n - 1
	}
	return string(b[:cut]), string(b[cut:])
}

// ByteSuffixLastN splits s so holdout is the last n bytes of the UTF-8 / byte sequence.
// If len(s) <= n, returns ("", s).
func ByteSuffixLastN(s string, n int) (prefix, holdout string) {
	if n <= 0 {
		return s, ""
	}
	b := []byte(s)
	if len(b) <= n {
		return "", s
	}
	split := len(b) - n
	return string(b[:split]), string(b[split:])
}

