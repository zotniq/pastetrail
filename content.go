package pastetrail

import (
	"encoding/base64"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"unicode/utf8"
)

func fill[T any](f *Field[T], reason string) {
	if f.Evidence == "" {
		*f = Missing[T](reason)
	}
}

func normalizeEvent(e *Event, limit int) {
	if e.Trigger.Kind == "" {
		e.Trigger.Kind = "unknown"
	}
	if e.Trigger.Evidence == "" {
		e.Trigger.Evidence = Inferred
	}
	fill(&e.Trigger.Automated, "input origin is not exposed")
	if e.Content.Evidence == "" {
		e.Content.Evidence = Unavailable
		e.Content.Reason = "clipboard data was not captured"
	}
	remaining := limit
	for i := range e.Content.Representations {
		r := &e.Content.Representations[i]
		data := []byte(r.Data)
		if r.Encoding == "base64" {
			decoded, err := base64.StdEncoding.DecodeString(r.Data)
			if err != nil {
				r.Data = ""
				r.Truncated = true
				e.Content.Incomplete = true
				continue
			}
			data = decoded
		}
		if r.Size < len(data) {
			r.Size = len(data)
		}
		if len(data) > remaining {
			data = data[:remaining]
			r.Truncated = true
		}
		if r.Encoding == "base64" {
			r.Data = base64.StdEncoding.EncodeToString(data)
		} else {
			for len(data) > 0 && !utf8.Valid(data) {
				data = data[:len(data)-1]
				r.Truncated = true
			}
			r.Encoding = "utf-8"
			r.Data = string(data)
		}
		remaining -= len(data)
		if r.Truncated {
			e.Content.Incomplete = true
		}
	}
	a := &e.Destination.Application
	fill(&a.Name, "application identity is unavailable")
	fill(&a.Executable, "executable is unavailable")
	fill(&a.PID, "process identity is unavailable")
	fill(&a.WindowID, "window identity is unavailable")
	fill(&a.WindowTitle, "window title is unavailable")
	fill(&a.BundleID, "application bundle identifier is unavailable on this platform")
	fill(&a.BundlePath, "application bundle path is unavailable on this platform")
	fill(&a.Bounds, "window bounds are unavailable")
	fill(&a.ContentBounds, "window content bounds are unavailable on this platform")
	fill(&a.MemoryUsage, "process resident memory usage is unavailable")
	if b := e.Destination.Browser; b != nil {
		fill(&b.TabID, "browser does not expose a tab ID")
		fill(&b.PID, "browser does not expose a tab renderer PID")
		fill(&b.Title, "tab title is unavailable")
		fill(&b.URL, "browser URL is unavailable without application integration")
		fill(&b.Frame, "frame is unavailable without application integration")
	}
	for i := range e.Destination.Contexts {
		c := &e.Destination.Contexts[i]
		fill(&c.Name, "context name is unavailable")
		fill(&c.PID, "process identity is unavailable")
		fill(&c.Host, "host identity is unavailable")
		fill(&c.TTY, "terminal identity is unavailable")
		fill(&c.Command, "command is unavailable")
		fill(&c.WorkingDirectory, "working directory is unavailable")
		fill(&c.Session, "session is unavailable")
		fill(&c.Pane, "pane is unavailable")
		fill(&c.Container, "container identity is unavailable")
	}
}

// FileReference inspects metadata with Lstat; it never opens a file's contents.
// Non-local URIs are preserved without accessing their hosts.
func FileReference(value string) File {
	f, local := fileLocation(value, runtime.GOOS == "windows")
	if !local {
		return f
	}
	info, err := os.Lstat(f.Path)
	if err != nil {
		f.MetadataError = err.Error()
		return f
	}
	size, dir, modified := info.Size(), info.IsDir(), info.ModTime().UTC()
	f.Size = &size
	f.Directory = &dir
	f.Modified = &modified
	return f
}

func fileLocation(value string, windows bool) (File, bool) {
	f := File{}
	if windows && (strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//")) {
		f.Path = value
		slash := strings.ReplaceAll(value, `\`, "/")
		f.Name = path.Base(slash)
		host, location, _ := strings.Cut(strings.TrimPrefix(slash, "//"), "/")
		if host != "?" && host != "." {
			f.URI = (&url.URL{Scheme: "file", Host: host, Path: "/" + location}).String()
		}
		f.MetadataError = "metadata unavailable for network or device path"
		return f, false
	}
	u := &url.URL{}
	var err error
	colon := strings.IndexByte(value, ':')
	if colon > 0 && !strings.ContainsAny(value[:colon], `/\`) && !(windows && colon == 1) {
		u, err = url.Parse(value)
	}
	if err != nil {
		f.URI = value
		f.MetadataError = err.Error()
		return f, false
	}
	if u.Scheme != "" {
		f.URI = value
		f.Name = path.Base(u.Path)
		if u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") {
			f.MetadataError = "metadata unavailable for non-local URI"
			return f, false
		}
		f.Path = u.Path
		if windows && len(f.Path) >= 3 && f.Path[0] == '/' && f.Path[2] == ':' {
			f.Path = f.Path[1:]
		}
	} else {
		f.Path = value
		slash := value
		if windows {
			slash = strings.ReplaceAll(value, `\`, "/")
			if len(slash) >= 3 && slash[1] == ':' && slash[2] == '/' {
				slash = "/" + slash
			}
		}
		f.URI = (&url.URL{Scheme: "file", Path: slash}).String()
	}
	if windows {
		f.Name = path.Base(strings.ReplaceAll(f.Path, `\`, "/"))
		f.Path = strings.ReplaceAll(f.Path, "/", `\`)
	} else {
		f.Name = filepath.Base(f.Path)
	}
	return f, true
}

func textRepresentation(media string, data []byte, limit int) Representation {
	r := Representation{MediaType: media, Size: len(data), Encoding: "utf-8"}
	if len(data) > limit {
		data = data[:limit]
		r.Truncated = true
	}
	if !utf8.Valid(data) {
		r.Encoding = "base64"
		r.Data = base64.StdEncoding.EncodeToString(data)
	} else {
		r.Data = string(data)
	}
	return r
}

func richMediaType(format string) string {
	switch strings.ToLower(format) {
	case "public.html", "text/html":
		return "text/html"
	case "html format", "application/x-windows-cf-html":
		return "application/x-windows-cf-html"
	case "public.rtf", "rich text format", "text/rtf", "application/rtf":
		return "text/rtf"
	default:
		media, _, err := mime.ParseMediaType(format)
		if err == nil && strings.HasPrefix(media, "text/") {
			return media
		}
		return ""
	}
}
