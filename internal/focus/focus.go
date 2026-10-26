// Package focus preserves accessibility metadata for the focused control.
package focus

import "github.com/zotniq/pastetrail/internal/model"

// Element contains metadata only; never request AXValue or selected text.
type Element struct {
	Role            string `json:"role"`
	RoleDescription string `json:"role_description"`
	Subrole         string `json:"subrole"`
	Identifier      string `json:"identifier"`
	Title           string `json:"title"`
	Description     string `json:"description"`
}

type Snapshot struct {
	Path      []Element        `json:"path"`
	Document  string           `json:"document"` // Only the focused control's AXDocument.
	Selection *model.TextRange `json:"selection"`
	Issue     string           `json:"issue"`
}

func FromSnapshot(n *Snapshot) *model.Focus {
	if n == nil {
		return nil
	}
	f := &model.Focus{
		Kind:        model.Missing[string]("focused control role is unavailable"),
		DocumentURI: model.Missing[string]("focused control does not expose a document URI"),
		Selection:   model.Missing[*model.TextRange]("focused control does not expose a text selection range"),
		Incomplete:  n.Issue != "", Reason: n.Issue,
	}
	for _, e := range n.Path {
		if e.Role == "AXWindow" || e.Role == "AXApplication" {
			break
		}
		f.Path = append(f.Path, model.UIElement{
			Role: attribute(e.Role), Subrole: attribute(e.Subrole), Identifier: attribute(e.Identifier),
			RoleDescription: attribute(e.RoleDescription),
			Title:           attribute(e.Title), Description: attribute(e.Description),
		})
	}
	if len(f.Path) == 0 {
		if n.Issue != "" {
			f.Kind = model.Missing[string](n.Issue)
		}
		return f
	}
	leaf := n.Path[0]
	kind := ""
	if leaf.Subrole == "AXSearchField" {
		kind = "search"
	} else if leaf.Role == "AXTextArea" || leaf.Role == "AXTextField" || leaf.Role == "AXComboBox" {
		kind = "text_input"
	}
	if kind != "" {
		f.Kind = model.Field[string]{Value: kind, Evidence: model.Inferred, Reason: "accessibility role identifies an input; its application-specific purpose is unavailable"}
	}
	if n.Document != "" && kind == "text_input" {
		f.DocumentURI = model.Known(n.Document)
	}
	if n.Selection != nil && n.Selection.Start >= 0 && n.Selection.Length >= 0 {
		f.Selection = model.Known(n.Selection)
	}
	return f
}

func attribute(value string) model.Field[string] {
	if value == "" {
		return model.Missing[string]("attribute is not exposed by the focused control")
	}
	return model.Known(value)
}
