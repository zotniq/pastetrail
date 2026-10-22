package terminal

import (
	"path/filepath"
	"strings"
)

func containerDestination(command string) string {
	a := strings.Fields(command)
	if len(a) < 3 {
		return ""
	}
	name := filepath.Base(a[0])
	if (name != "docker" && name != "podman") || a[1] != "exec" {
		return ""
	}
	for i := 2; i < len(a); i++ {
		v := a[i]
		if strings.ContainsAny(v, "'\"\\") {
			return ""
		}
		if v == "--" {
			if i+1 < len(a) && !strings.ContainsAny(a[i+1], "'\"\\") {
				return a[i+1]
			}
			return ""
		}
		if !strings.HasPrefix(v, "-") {
			return v
		}
		if strings.HasPrefix(v, "--") {
			option, _, attached := strings.Cut(v, "=")
			switch option {
			case "--user", "--workdir", "--env", "--env-file", "--detach-keys", "--preserve-fds":
				if !attached {
					i++
				}
			case "--detach", "--interactive", "--tty", "--privileged":
			default:
				return ""
			}
			continue
		}
		for j := 1; j < len(v); j++ {
			if strings.ContainsRune("uwe", rune(v[j])) {
				if j == len(v)-1 {
					i++
				}
				break
			}
			if !strings.ContainsRune("dit", rune(v[j])) {
				return ""
			}
		}
	}
	return ""
}
