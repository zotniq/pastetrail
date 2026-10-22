package pastetrail

import (
	"fmt"
	"os"
)

func processCWD(pid int) (string, error) { return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)) }
