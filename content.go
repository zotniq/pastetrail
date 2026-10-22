package pastetrail

import "github.com/zotniq/pastetrail/internal/content"

// FileReference inspects metadata with Lstat; it never opens a file's contents.
// Non-local URIs are preserved without accessing their hosts.
func FileReference(value string) File { return content.FileReference(value) }
