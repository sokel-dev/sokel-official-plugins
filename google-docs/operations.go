package main

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const docsOrigin = "https://docs.googleapis.com"

var googleID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func docPath(id string) (string, error) {
	if !googleID.MatchString(id) {
		return "", fmt.Errorf("document_id must be a Google document ID")
	}
	return "/v1/documents/" + id, nil
}
func tabID(id string) error {
	if strings.TrimSpace(id) == "" || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("tab_id is required; get the document to find its tab IDs")
	}
	return nil
}

// Typed projection: styles and non-text elements are intentionally omitted.
type structuralElement struct {
	EndIndex  int `json:"endIndex"`
	Paragraph *struct {
		Elements []struct {
			TextRun *struct {
				Content string `json:"content"`
			} `json:"textRun"`
		} `json:"elements"`
	} `json:"paragraph"`
	Table *struct {
		Rows []struct {
			Cells []struct {
				Content []structuralElement `json:"content"`
			} `json:"tableCells"`
		} `json:"tableRows"`
	} `json:"table"`
	TableOfContents *struct {
		Content []structuralElement `json:"content"`
	} `json:"tableOfContents"`
}
type documentTab struct {
	Properties struct {
		ID    string `json:"tabId"`
		Title string `json:"title"`
	} `json:"tabProperties"`
	Document struct {
		Body struct {
			Content []structuralElement `json:"content"`
		} `json:"body"`
	} `json:"documentTab"`
	Children []documentTab `json:"childTabs"`
}

func bodyText(content []structuralElement, b *strings.Builder) {
	for _, e := range content {
		if e.Paragraph != nil {
			for _, p := range e.Paragraph.Elements {
				if p.TextRun != nil {
					b.WriteString(p.TextRun.Content)
				}
			}
		}
		if e.Table != nil {
			for _, r := range e.Table.Rows {
				for _, c := range r.Cells {
					bodyText(c.Content, b)
				}
			}
		}
		if e.TableOfContents != nil {
			bodyText(e.TableOfContents.Content, b)
		}
	}
}
func collectTabs(tabs []documentTab, parent string, out *[]DocumentTab) {
	for _, t := range tabs {
		var text strings.Builder
		bodyText(t.Document.Body.Content, &text)
		end := 0
		for _, e := range t.Document.Body.Content {
			if e.EndIndex > end {
				end = e.EndIndex
			}
		}
		*out = append(*out, DocumentTab{ID: t.Properties.ID, Title: t.Properties.Title, ParentID: parent, Text: text.String(), EndIndex: end})
		collectTabs(t.Children, t.Properties.ID, out)
	}
}
func (a *api) get(ctx context.Context, in *GetDocumentIn) (*GetDocumentOut, error) {
	p, e := docPath(in.DocumentID)
	if e != nil {
		return nil, e
	}
	var r struct {
		ID       string        `json:"documentId"`
		Title    string        `json:"title"`
		Revision string        `json:"revisionId"`
		Tabs     []documentTab `json:"tabs"`
	}
	if e = a.json(ctx, "GET", docsOrigin, p, url.Values{"includeTabsContent": {"true"}}, nil, &r); e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Google returned a document without its ID")
	}
	if r.Tabs == nil {
		return nil, fmt.Errorf("Google omitted the requested document tabs")
	}
	out := &GetDocumentOut{DocumentID: r.ID, Title: r.Title, RevisionID: r.Revision, Tabs: []DocumentTab{}}
	collectTabs(r.Tabs, "", &out.Tabs)
	return out, nil
}
func (a *api) create(ctx context.Context, in *CreateDocumentIn) (*CreateDocumentOut, error) {
	if strings.TrimSpace(in.Title) == "" {
		return nil, fmt.Errorf("title is required")
	}
	var r struct {
		ID    string `json:"documentId"`
		Title string `json:"title"`
	}
	if e := a.json(ctx, "POST", docsOrigin, "/v1/documents", nil, map[string]string{"title": in.Title}, &r); e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Google returned a document without its ID")
	}
	return &CreateDocumentOut{DocumentID: r.ID, Title: r.Title}, nil
}

type batchResponse struct {
	ID           string `json:"documentId"`
	WriteControl struct {
		Revision string `json:"requiredRevisionId"`
	} `json:"writeControl"`
	Replies []struct {
		ReplaceAllText *struct {
			Count int `json:"occurrencesChanged"`
		} `json:"replaceAllText"`
	} `json:"replies"`
}

func (a *api) batch(ctx context.Context, id, revision string, request any) (*batchResponse, error) {
	p, e := docPath(id)
	if e != nil {
		return nil, e
	}
	b := map[string]any{"requests": []any{request}}
	if revision != "" {
		b["writeControl"] = map[string]string{"requiredRevisionId": revision}
	}
	var r batchResponse
	if e = a.json(ctx, "POST", docsOrigin, p+":batchUpdate", nil, b, &r); e != nil {
		return nil, e
	}
	if r.ID == "" {
		return nil, fmt.Errorf("Google returned a write response without its document ID")
	}
	if len(r.Replies) != 1 {
		return nil, fmt.Errorf("Google returned an incomplete write reply; inspect the document before retrying")
	}
	return &r, nil
}
func (a *api) insert(ctx context.Context, in *InsertTextIn) (*InsertTextOut, error) {
	if e := tabID(in.TabID); e != nil {
		return nil, e
	}
	if in.Text == "" || in.Index < 0 {
		return nil, fmt.Errorf("text must be nonempty and index must not be negative")
	}
	req := map[string]any{"text": in.Text}
	if in.Index == 0 {
		req["endOfSegmentLocation"] = map[string]string{"tabId": in.TabID}
	} else {
		req["location"] = map[string]any{"tabId": in.TabID, "index": in.Index}
	}
	r, e := a.batch(ctx, in.DocumentID, in.RequiredRevisionID, map[string]any{"insertText": req})
	if e != nil {
		return nil, e
	}
	return &InsertTextOut{DocumentID: r.ID, RevisionID: r.WriteControl.Revision}, nil
}
func (a *api) replace(ctx context.Context, in *ReplaceTextIn) (*ReplaceTextOut, error) {
	if e := tabID(in.TabID); e != nil {
		return nil, e
	}
	if in.Find == "" {
		return nil, fmt.Errorf("find must not be empty")
	}
	r, e := a.batch(ctx, in.DocumentID, in.RequiredRevisionID, map[string]any{"replaceAllText": map[string]any{"containsText": map[string]any{"text": in.Find, "matchCase": in.MatchCase}, "replaceText": in.Replacement, "tabsCriteria": map[string]any{"tabIds": []string{in.TabID}}}})
	if e != nil {
		return nil, e
	}
	n := 0
	for _, reply := range r.Replies {
		if reply.ReplaceAllText == nil {
			return nil, fmt.Errorf("Google omitted the replacement result; inspect the document before retrying")
		}
		n += reply.ReplaceAllText.Count
	}
	return &ReplaceTextOut{DocumentID: r.ID, RevisionID: r.WriteControl.Revision, OccurrencesChanged: n}, nil
}
func (a *api) delete(ctx context.Context, in *DeleteTextIn) (*DeleteTextOut, error) {
	if e := tabID(in.TabID); e != nil {
		return nil, e
	}
	if in.StartIndex < 1 || in.EndIndex <= in.StartIndex {
		return nil, fmt.Errorf("the delete range requires 1 <= start_index < end_index; do not include the final newline")
	}
	r, e := a.batch(ctx, in.DocumentID, in.RequiredRevisionID, map[string]any{"deleteContentRange": map[string]any{"range": map[string]any{"tabId": in.TabID, "startIndex": in.StartIndex, "endIndex": in.EndIndex}}})
	if e != nil {
		return nil, e
	}
	return &DeleteTextOut{DocumentID: r.ID, RevisionID: r.WriteControl.Revision}, nil
}
