package focus

import (
	"testing"

	"github.com/zotniq/pastetrail/internal/model"
)

func TestFocusMetadata(t *testing.T) {
	if FromSnapshot(nil) != nil {
		t.Fatal("absent native focus must stay absent")
	}
	for _, n := range []Snapshot{{}, {Issue: "Accessibility permission required"}, {Path: []Element{{Role: "AXWindow"}}, Document: "file:///wrong.go"}} {
		f := FromSnapshot(&n)
		if len(f.Path) != 0 || f.Kind.Evidence != model.Unavailable || f.DocumentURI.Value != "" || f.Incomplete != (n.Issue != "") {
			t.Fatal(f)
		}
		if n.Issue != "" && f.Kind.Reason != n.Issue {
			t.Fatal(f.Kind)
		}
	}
	n := Snapshot{Path: []Element{{Role: "AXTextArea", Title: "Editor"}}, Document: "file:///project/main.go", Selection: &model.TextRange{Start: 0, Length: 0}, Issue: "ancestry truncated"}
	f := FromSnapshot(&n)
	if f.DocumentURI.Value != n.Document || f.DocumentURI.Evidence != model.Observed || f.Selection.Value != n.Selection || !f.Incomplete || f.Reason != n.Issue {
		t.Fatal(f)
	}
	if f.Path[0].Identifier.Evidence != model.Unavailable || f.Path[0].Title.Evidence != model.Observed {
		t.Fatal(f.Path)
	}
	for _, e := range []Element{{Title: "Terminal"}, {Subrole: "AXSearchField"}} {
		n.Path[0] = e
		if got := FromSnapshot(&n); got.DocumentURI.Value != "" {
			t.Fatalf("non-editor attributed to a document: %+v", got)
		}
	}
	n.Selection.Start = -1
	if FromSnapshot(&n).Selection.Evidence != model.Unavailable {
		t.Fatal("invalid selection accepted")
	}
}
