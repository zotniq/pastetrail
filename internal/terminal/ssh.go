package terminal

import (
	"path/filepath"
	"strings"
)

func sshDestination(command string) string {
	args := strings.Fields(command)
	if len(args) < 2 || filepath.Base(args[0]) != "ssh" {
		return ""
	}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.ContainsAny(a, "'\"\\") {
			return ""
		}
		if a == "--" {
			if i+1 < len(args) && !strings.ContainsAny(args[i+1], "'\"\\") {
				return args[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(a, "-") {
			return a
		}
		for j := 1; j < len(a); j++ {
			if strings.ContainsRune("BbcDEeFIiJLlmOoPpQRSWw", rune(a[j])) {
				if j == len(a)-1 {
					i++
				}
				break
			}
			if !strings.ContainsRune("1246AaCfGgKkMNnqsTtVvXxYy", rune(a[j])) {
				return ""
			}
		}
	}
	return ""
}
