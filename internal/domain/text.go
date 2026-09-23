package domain

import "strings"

// Recortar recorta s a n bytes y añade "..." si hace falta.
func Recortar(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
