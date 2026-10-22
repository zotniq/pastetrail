//go:build darwin && cgo

package pastetrail

/*
#include <stdlib.h>
#include <string.h>
#include <libproc.h>
static char *pastetrail_cwd(int pid) {
    struct proc_vnodepathinfo info;
    if (proc_pidinfo(pid, PROC_PIDVNODEPATHINFO, 0, &info, sizeof(info)) != sizeof(info)) return NULL;
    return strdup(info.pvi_cdir.vip_path);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func processCWD(pid int) (string, error) {
	p := C.pastetrail_cwd(C.int(pid))
	if p == nil {
		return "", fmt.Errorf("working directory is inaccessible")
	}
	defer C.free(unsafe.Pointer(p))
	return C.GoString(p), nil
}
