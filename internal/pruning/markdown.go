package pruning

import "strings"

// NormalizeWhitespace trims excessive whitespace from text.
func NormalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
