package terminalipc

import (
	"os"
	"path/filepath"
)

func DefaultPath() (string, error) {
	if path := os.Getenv("PASTETRAIL_SOCKET"); path != "" {
		return filepath.Abs(path)
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pastetrail", "relay.sock"), nil
}
