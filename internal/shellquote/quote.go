package shellquote

import "strings"

// Quote produces one literal POSIX shell word.
func Quote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'" }
