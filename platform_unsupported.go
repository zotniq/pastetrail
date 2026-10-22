//go:build !cgo || (!darwin && !windows && !linux)

package pastetrail

func newPlatformSource(Options) Source {
	return unavailableSource{reason: "native monitoring requires a supported OS and a build with CGO_ENABLED=1"}
}
