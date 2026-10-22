//go:build darwin && cgo

package platform

/*
#cgo LDFLAGS: -framework CoreServices
#include <stddef.h>
int pt_terminal_tty(int pid, int iterm, char *tty, size_t size);
int pt_browser_url(int pid, int kind, char *url, size_t size);
*/
import "C"

func TerminalTTY(pid int, iterm bool) (string, int) {
	var buffer [1024]C.char
	kind := C.int(0)
	if iterm {
		kind = 1
	}
	status := C.pt_terminal_tty(C.int(pid), kind, &buffer[0], C.size_t(len(buffer)))
	return C.GoString(&buffer[0]), int(status)
}

func BrowserURL(pid, kind int) (string, int) {
	var buffer [8192]C.char
	status := C.pt_browser_url(C.int(pid), C.int(kind), &buffer[0], C.size_t(len(buffer)))
	return C.GoString(&buffer[0]), int(status)
}
