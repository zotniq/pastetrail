//go:build cgo && (darwin || linux)

package enrollment

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func startupPaths(home string) ([]string, error) {
	if !filepath.IsAbs(home) {
		return nil, errors.New("home directory must be absolute for automatic terminal setup")
	}
	paths := []string{filepath.Join(home, ".zshenv"), filepath.Join(home, ".bashrc")}
	for _, key := range []string{"ZDOTDIR", "USER_ZDOTDIR", "JETBRAINS_INTELLIJ_ORIGINAL_ZDOTDIR"} {
		dir := os.Getenv(key)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("%s must be absolute for automatic terminal setup", key)
		}
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return nil, err
		}
		if !integrationPath(resolved) {
			paths = append(paths, filepath.Join(resolved, ".zshenv"))
		}
	}
	profile := filepath.Join(home, ".bash_profile")
	for _, name := range []string{".bash_profile", ".bash_login", ".profile"} {
		p := filepath.Join(home, name)
		if _, err := os.Stat(p); err == nil {
			profile = p
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	return append(paths, profile), nil
}

func integrationPath(path string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(filepath.Separator)) {
		part = strings.ToLower(part)
		if strings.HasSuffix(part, ".app") || part == "shell-integrations" || strings.Contains(part, "shellintegration") || strings.HasPrefix(part, "vscode-zsh") {
			return true
		}
	}
	for _, key := range []string{"VSCODE_ZDOTDIR", "JETBRAINS_INTELLIJ_ZSH_DIR"} {
		if dir := os.Getenv(key); dir != "" {
			if resolved, err := filepath.EvalSymlinks(dir); err == nil {
				rel, err := filepath.Rel(resolved, path)
				if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
					return true
				}
			}
		}
	}
	return false
}
