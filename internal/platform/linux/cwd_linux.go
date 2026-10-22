package linux

import (
	"fmt"
	"os"
)

func CWD(pid int) (string, error) { return os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid)) }
