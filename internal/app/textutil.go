package app

import "strings"

func normalizeOneLine(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}
