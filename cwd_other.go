//go:build (!darwin && !linux) || (darwin && !cgo)

package pastetrail

import "fmt"

func processCWD(int) (string, error) {
	return "", fmt.Errorf("working directory inspection is unavailable in this build")
}
