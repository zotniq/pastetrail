//go:build darwin && cgo

package macos

/*
#cgo LDFLAGS: -framework CoreServices
#include <stddef.h>
int pt_browser_url(int pid, int kind, char *url, size_t size);
*/
import "C"

func BrowserURL(pid, kind int) (string, int) {
	var buffer [8192]C.char
	status := C.pt_browser_url(C.int(pid), C.int(kind), &buffer[0], C.size_t(len(buffer)))
	return C.GoString(&buffer[0]), int(status)
}
