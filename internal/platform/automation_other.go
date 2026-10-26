//go:build !darwin || !cgo

package platform

func BrowserURL(int, int) (string, int) { return "", -1 }
