package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/tianyancha/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// Exact integer identifiers never pass through float64.
type exactInteger string

var digits = regexp.MustCompile(`^[0-9]+$`)

func (v *exactInteger) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*v = ""
		return nil
	}
	var s string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return fmt.Errorf("invalid integer")
		}
	} else {
		s = string(raw)
	}
	if !digits.MatchString(s) {
		return fmt.Errorf("invalid integer")
	}
	*v = exactInteger(s)
	return nil
}

// Establishment dates before 1970 are valid signed millisecond timestamps.
type exactTimestamp string

var signedDigits = regexp.MustCompile(`^-?[0-9]+$`)

func (v *exactTimestamp) UnmarshalJSON(raw []byte) error {
	if bytes.Equal(raw, []byte("null")) {
		*v = ""
		return nil
	}
	var s string
	if len(raw) > 0 && raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return fmt.Errorf("invalid timestamp")
		}
	} else {
		s = string(raw)
	}
	if !signedDigits.MatchString(s) {
		return fmt.Errorf("invalid timestamp")
	}
	*v = exactTimestamp(s)
	return nil
}

func (a *api) call(ctx context.Context, path string, q url.Values) (json.RawMessage, bool, error) {
	raw, e := a.get(ctx, path, q)
	if e != nil {
		return nil, false, e
	}
	var env struct {
		Code   *int            `json:"error_code"`
		Result json.RawMessage `json:"result"`
	}
	if json.Unmarshal(raw, &env) != nil || env.Code == nil {
		return nil, false, fmt.Errorf("Tianyancha response has no valid error code")
	}
	if *env.Code == 300000 {
		return nil, true, nil
	}
	if *env.Code != 0 {
		hint := "request failed"
		switch *env.Code {
		case 300002, 300003:
			hint = "account or token is invalid or expired"
		case 300004:
			hint = "rate limit reached; retry later"
		case 300005:
			hint = "the account has no permission for this API"
		case 300006:
			hint = "account balance is insufficient"
		case 300007:
			hint = "remaining API calls are exhausted"
		case 300011:
			hint = "the caller IP is not allowed"
		}
		return nil, false, fmt.Errorf("Tianyancha error %d: %s", *env.Code, hint)
	}
	if len(env.Result) == 0 || bytes.Equal(env.Result, []byte("null")) {
		return nil, false, fmt.Errorf("Tianyancha success response is missing its result")
	}
	return env.Result, false, nil
}
func keyword(v string) error {
	if strings.TrimSpace(v) == "" || len(v) > 1024 || strings.ContainsAny(v, "\r\n\x00") {
		return fmt.Errorf("company keyword is required and must be at most 1024 bytes without control characters")
	}
	return nil
}
func (a *api) GetCompany(ctx context.Context, in *GetCompanyIn) (*GetCompanyOut, error) {
	if e := keyword(in.Keyword); e != nil {
		return nil, e
	}
	raw, empty, e := a.call(ctx, "/services/v4/open/baseinfoV3", url.Values{"keyword": {in.Keyword}})
	if e != nil {
		return nil, e
	}
	if empty {
		return nil, fmt.Errorf("no company matched the supplied keyword")
	}
	var w struct {
		ID                  exactInteger   `json:"id"`
		Name                string         `json:"name"`
		Status              string         `json:"regStatus"`
		CreditCode          string         `json:"creditCode"`
		LegalRepresentative string         `json:"legalPersonName"`
		Capital             string         `json:"regCapital"`
		Industry            string         `json:"industry"`
		Address             string         `json:"regLocation"`
		BusinessScope       string         `json:"businessScope"`
		EstablishedAt       exactTimestamp `json:"estiblishTime"`
		Staff               *struct {
			Total  *int `json:"total"`
			Result []struct {
				ID        exactInteger `json:"id"`
				Name      string       `json:"name"`
				Positions []string     `json:"typeJoin"`
			} `json:"result"`
		} `json:"staffList"`
	}
	if json.Unmarshal(raw, &w) != nil || w.ID == "" || w.Name == "" {
		return nil, fmt.Errorf("Tianyancha returned invalid company data")
	}
	v := schema.Company{ID: string(w.ID), Name: w.Name, Status: w.Status, CreditCode: w.CreditCode, LegalRepresentative: w.LegalRepresentative, Capital: w.Capital, Industry: w.Industry, Address: w.Address, BusinessScope: w.BusinessScope, EstablishedAt: string(w.EstablishedAt), Staff: []schema.Staff{}}
	if w.Staff != nil {
		if w.Staff.Total == nil || *w.Staff.Total < 0 || *w.Staff.Total < len(w.Staff.Result) || (*w.Staff.Total > 0 && w.Staff.Result == nil) {
			return nil, fmt.Errorf("Tianyancha returned invalid key personnel data")
		}
		v.StaffTotal = *w.Staff.Total
		for _, s := range w.Staff.Result {
			if s.ID == "" || s.Name == "" {
				return nil, fmt.Errorf("Tianyancha key personnel entry is missing its ID or name")
			}
			positions := s.Positions
			if positions == nil {
				positions = []string{}
			}
			v.Staff = append(v.Staff, schema.Staff{ID: string(s.ID), Name: s.Name, Positions: positions})
		}
	}
	return &GetCompanyOut{Company: v}, nil
}
func (a *api) ListCompanyChanges(ctx context.Context, in *ListCompanyChangesIn) (*ListCompanyChangesOut, error) {
	if e := keyword(in.Keyword); e != nil {
		return nil, e
	}
	page, size := 1, 20
	if in.Page.Page != nil {
		page = *in.Page.Page
	}
	if in.Page.PageSize != nil {
		size = *in.Page.PageSize
	}
	if page < 1 || page > 10000 || size < 1 || size > 20 {
		return nil, fmt.Errorf("page number must be 1..10000 and page size must be 1..20")
	}
	raw, empty, e := a.call(ctx, "/services/open/ic/changeinfo/2.0", url.Values{"keyword": {in.Keyword}, "pageNum": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(size)}})
	if e != nil {
		return nil, e
	}
	out := &ListCompanyChangesOut{Changes: []schema.Change{}}
	if empty {
		return out, nil
	}
	var w struct {
		Total *int            `json:"total"`
		Items []schema.Change `json:"items"`
	}
	if json.Unmarshal(raw, &w) != nil || w.Total == nil || *w.Total < 0 || w.Items == nil || len(w.Items) > size || len(w.Items) > *w.Total {
		return nil, fmt.Errorf("Tianyancha returned invalid registration changes")
	}
	if (page-1)*size < *w.Total && len(w.Items) == 0 {
		return nil, fmt.Errorf("Tianyancha returned an empty page before the reported total")
	}
	for _, row := range w.Items {
		if row.Item == "" {
			return nil, fmt.Errorf("Tianyancha change entry is missing the changed item")
		}
	}
	out.Changes = w.Items
	out.Total = *w.Total
	if page*size < *w.Total {
		if len(w.Items) < size {
			return nil, fmt.Errorf("Tianyancha returned a short page before the reported total")
		}
		if page == 10000 {
			return nil, fmt.Errorf("registration change pagination exceeds the plugin limit of 10000 pages")
		}
		out.HasMore = true
		out.NextPage = page + 1
	}
	return out, nil
}
