package content

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
)

func TestNormalize(t *testing.T) {
	e := model.Event{Destination: model.Destination{Browser: &model.Browser{}, Focus: &model.Focus{Path: []model.UIElement{{}}}, Contexts: []model.Context{{}}}}
	NormalizeEvent(&e, 10)
	if e.Trigger.Kind != "unknown" || e.Trigger.Evidence != model.Inferred || e.Trigger.Automated.Evidence != model.Unavailable || e.Content.Evidence != model.Unavailable {
		t.Fatal(e)
	}
	if e.Destination.Application.PID.Evidence != model.Unavailable || e.Destination.Browser.PID.Evidence != model.Unavailable || e.Destination.Browser.URL.Evidence != model.Unavailable || e.Destination.Contexts[0].Host.Evidence != model.Unavailable {
		t.Fatal(e.Destination)
	}
	f := model.Known(42)
	focus := e.Destination.Focus
	if focus.Kind.Evidence != model.Unavailable || focus.DocumentURI.Evidence != model.Unavailable || focus.Selection.Evidence != model.Unavailable || focus.Path[0].Role.Evidence != model.Unavailable || focus.Path[0].RoleDescription.Evidence != model.Unavailable {
		t.Fatal(focus)
	}
	fill(&f, "missing")
	if f.Value != 42 || f.Evidence != model.Observed {
		t.Fatal(f)
	}
}

func TestContentLimits(t *testing.T) {
	for _, tt := range []struct {
		name  string
		reps  []model.Representation
		limit int
		want  []string
	}{
		{"unicode", []model.Representation{{Data: "a😀b"}}, 4, []string{"a"}},
		{"aggregate", []model.Representation{{Data: "abc"}, {Data: "def"}}, 5, []string{"abc", "de"}},
		{"binary", []model.Representation{{Encoding: "base64", Data: base64.StdEncoding.EncodeToString([]byte{0, 1, 2, 3})}}, 2, []string{"AAE="}},
		{"bad encoding", []model.Representation{{Encoding: "base64", Data: "!!!"}}, 5, []string{""}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			e := model.Event{Content: model.Content{Representations: tt.reps}}
			NormalizeEvent(&e, tt.limit)
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
	r := TextRepresentation("text/plain", []byte("hello"), 3)
	if r.Data != "hel" || r.Size != 5 || !r.Truncated || r.Encoding != "utf-8" {
		t.Fatal(r)
	}
	r = TextRepresentation("text/rtf", []byte{0xff}, 10)
	if r.Encoding != "base64" || r.Data != "/w==" {
		t.Fatal(r)
	}
	for input, want := range map[string]string{"public.html": "text/html", "HTML Format": "application/x-windows-cf-html", "public.rtf": "text/rtf", "Rich Text Format": "text/rtf", "text/plain; charset=utf-8": "text/plain", "image/png": "", "not a format": ""} {
		if got := RichMediaType(input); got != want {
			t.Errorf("%s: %s", input, got)
		}
	}
}

func TestWindowsFileLocations(t *testing.T) {
	for _, value := range []string{`C:\Users\test\report with spaces.txt`, "file:///C:/Users/test/report%20with%20spaces.txt", "file://localhost/C:/Users/test/report%20with%20spaces.txt"} {
		f, local := fileLocation(value, true)
		if !local || f.Path != `C:\Users\test\report with spaces.txt` || f.Name != "report with spaces.txt" || !strings.Contains(f.URI, "/C:/Users/test/report%20with%20spaces.txt") {
			t.Fatalf("%q: %+v", value, f)
		}
	}
	for _, value := range []string{`\\server\share\report.txt`, "//server/share/report.txt", `\\?\C:\report.txt`} {
		f, local := fileLocation(value, true)
		if local || f.MetadataError == "" || f.Name != "report.txt" {
			t.Fatalf("%q: %+v", value, f)
		}
	}
	f, local := fileLocation(`\\server\share\100% report.txt`, true)
	if local || f.URI != "file://server/share/100%25%20report.txt" {
		t.Fatal(f)
	}
}

func TestLiteralFilePathCharacters(t *testing.T) {
	name := filepath.Join(t.TempDir(), "100% #report.txt")
	if err := os.WriteFile(name, nil, 0600); err != nil {
		t.Fatal(err)
	}
	f := FileReference(name)
	if f.MetadataError != "" || f.Name != "100% #report.txt" || f.Path != name || !strings.Contains(f.URI, "100%25%20%23report.txt") {
		t.Fatal(f)
	}
	if roundtrip := FileReference(f.URI); roundtrip.Path != name || roundtrip.MetadataError != "" {
		t.Fatal(roundtrip)
	}
}
