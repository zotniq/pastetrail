//go:build darwin && cgo

package pastetrail

/*
#cgo LDFLAGS: -framework CoreServices
#include <stddef.h>
int pt_terminal_tty(int pid, int iterm, char *tty, size_t size);
*/
import "C"

func nativeTerminalTTY(pid int, iterm bool) (string, int) {
	var buffer [1024]C.char
	kind := C.int(0)
	if iterm {
		kind = 1
	}
	status := C.pt_terminal_tty(C.int(pid), kind, &buffer[0], C.size_t(len(buffer)))
	return C.GoString(&buffer[0]), int(status)
}
