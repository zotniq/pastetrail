package pastetrail

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	e := Event{Destination: Destination{Browser: &Browser{}, Contexts: []Context{{}}}}
	normalizeEvent(&e, 10)
	if e.Trigger.Kind != "unknown" || e.Trigger.Evidence != Inferred || e.Trigger.Automated.Evidence != Unavailable || e.Content.Evidence != Unavailable {
		t.Fatal(e)
	}
	if e.Destination.Application.PID.Evidence != Unavailable || e.Destination.Browser.URL.Evidence != Unavailable || e.Destination.Contexts[0].Host.Evidence != Unavailable {
		t.Fatal(e.Destination)
	}
	f := Known(42)
	fill(&f, "missing")
	if f.Value != 42 || f.Evidence != Observed {
		t.Fatal(f)
	}
}

func TestContentLimits(t *testing.T) {
	for _, tt := range []struct {
		name  string
		reps  []Representation
		limit int
		want  []string
	}{
		{"unicode", []Representation{{Data: "a😀b"}}, 4, []string{"a"}},
		{"aggregate", []Representation{{Data: "abc"}, {Data: "def"}}, 5, []string{"abc", "de"}},
		{"binary", []Representation{{Encoding: "base64", Data: base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})}}, 2, []string{"AAE="}},
		{"bad encoding", []Representation{{Encoding: "base64", Data: "!!!"}}, 5, []string{""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := Event{Content: Content{Representations: tt.reps}}
			normalizeEvent(&e, tt.limit)
			if !e.Content.Incomplete {
				t.Fatal("truncation was not reported")
			}
			for i, w := range tt.want {
				if e.Content.Representations[i].Data != w {
					t.Fatalf("%q != %q", e.Content.Representations[i].Data, w)
				}
			}
		})
	}
}

func TestFileReferences(t *testing.T) {
	dir := t.TempDir()
	name := filepath.Join(dir, "report with spaces.txt")
	if err := os.WriteFile(name, []byte("contents must not be captured"), 0600); err != nil {
		t.Fatal(err)
	}
	f := FileReference(name)
	if f.Name != "report with spaces.txt" || f.Path != name || f.Size == nil || *f.Size != int64(len("contents must not be captured")) || f.Directory == nil || *f.Directory || f.Modified == nil || f.MetadataError != "" {
		t.Fatalf("%+v", f)
	}
	uriFile := FileReference(f.URI)
	if uriFile.Path != name || uriFile.MetadataError != "" {
		t.Fatal(uriFile)
	}
	if d := FileReference(dir); d.Directory == nil || !*d.Directory {
		t.Fatal(d)
	}
	if f := FileReference(filepath.Join(dir, "missing")); f.MetadataError == "" {
		t.Fatal(f)
	}
	for _, uri := range []string{"https://example.com/report.txt", "file://remote/share/report.txt"} {
		f := FileReference(uri)
		if f.Name != "report.txt" || f.Path != "" || f.Size != nil || !strings.Contains(f.MetadataError, "non-local") {
			t.Fatal(f)
		}
	}
	if f := FileReference("file:///%bad%%"); f.MetadataError == "" {
		t.Fatal(f)
	}
}

func TestRepresentations(t *testing.T) {
	r := textRepresentation("text/plain", []byte("hello"), 3)
	if r.Data != "hel" || r.Size != 5 || !r.Truncated || r.Encoding != "utf-8" {
		t.Fatal(r)
	}
	r = textRepresentation("text/rtf", []byte{0xff}, 10)
	if r.Encoding != "base64" || r.Data != "/w==" {
		t.Fatal(r)
	}
	for input, want := range map[string]string{"public.html": "text/html", "HTML Format": "text/html", "public.rtf": "text/rtf", "Rich Text Format": "text/rtf", "text/plain; charset=utf-8": "text/plain", "image/png": "", "not a format": ""} {
		if got := richMediaType(input); got != want {
			t.Errorf("%s: %s", input, got)
		}
	}
}
