//go:build darwin && cgo

package process

/*
#include <errno.h>
#include <libproc.h>
#include <stdlib.h>
#include <string.h>
#include <sys/sysctl.h>

static int pt_process_command(int pid, char *path, int path_size, char **args, size_t *size) {
    if (proc_pidpath(pid, path, path_size) <= 0) return errno ? errno : ESRCH;
    int argmax = 0;
    size_t n = sizeof(argmax);
    int max_mib[] = { CTL_KERN, KERN_ARGMAX };
    if (sysctl(max_mib, 2, &argmax, &n, NULL, 0) < 0) return errno;
    if (argmax <= 0 || argmax > (4 << 20)) return E2BIG;
    char *data = malloc(argmax);
    if (!data) return ENOMEM;
    n = argmax;
    int mib[] = { CTL_KERN, KERN_PROCARGS2, pid };
    if (sysctl(mib, 3, data, &n, NULL, 0) < 0) { int e = errno; free(data); return e; }
    if (n < sizeof(int)) { free(data); return EINVAL; }
    int argc = 0;
    memcpy(&argc, data, sizeof(argc));
    char *p = data + sizeof(argc), *end = data + n;
    while (p < end && *p) p++;
    while (p < end && !*p) p++;
    char *start = p;
    if (argc < 1 || argc > 4096) { free(data); return EINVAL; }
    for (int i = 0; i < argc; i++) {
        while (p < end && *p) p++;
        if (p == end) { free(data); return EINVAL; }
        p++;
    }
    *size = p - start;
    memmove(data, start, *size);
    *args = data;
    return 0;
}
*/
import "C"

import (
	"bytes"
	"syscall"
	"unsafe"
)

func ReadCommand(pid int) (Command, error) {
	var path [4096]C.char
	var data *C.char
	var size C.size_t
	if status := C.pt_process_command(C.int(pid), &path[0], C.int(len(path)), &data, &size); status != 0 {
		return Command{}, syscall.Errno(status)
	}
	defer C.free(unsafe.Pointer(data))
	command := Command{Path: C.GoString(&path[0])}
	for _, arg := range bytes.Split(C.GoBytes(unsafe.Pointer(data), C.int(size))[:int(size)-1], []byte{0}) {
		command.Args = append(command.Args, string(arg))
	}
	return command, nil
}
