//go:build (!darwin && !linux) || (darwin && !cgo)

package platform

import "fmt"

func CWD(int) (string, error) {
	return "", fmt.Errorf("working directory inspection is unavailable in this build")
}
