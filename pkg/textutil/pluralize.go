package textutil

// Pluralize returns singular when count == 1, otherwise plural.
func Pluralize(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}
