package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"github.com/sokel-dev/sokel-official-plugins/mailchimp/schema"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var safeID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func audiencePath(id string) (string, error) {
	if !safeID.MatchString(id) {
		return "", fmt.Errorf("audience ID must contain only letters, digits, underscores or hyphens")
	}
	return "/3.0/lists/" + id, nil
}
func memberPath(id, address string) (string, error) {
	p, e := audiencePath(id)
	if e != nil {
		return "", e
	}
	if e = email(address); e != nil {
		return "", e
	}
	h := md5.Sum([]byte(strings.ToLower(address)))
	return p + "/members/" + hex.EncodeToString(h[:]), nil
}
func validStatus(s string, archived bool) bool {
	switch s {
	case "subscribed", "unsubscribed", "cleaned", "pending", "transactional":
		return true
	case "archived":
		return archived
	}
	return false
}
func memberValid(m *schema.Member) error {
	if !regexp.MustCompile(`^[a-f0-9]{32}$`).MatchString(m.ID) || email(m.EmailAddress) != nil || !validStatus(m.Status, true) {
		return fmt.Errorf("Mailchimp returned an invalid member")
	}
	if m.MergeFields == nil {
		m.MergeFields = map[string]any{}
	}
	return nil
}
func audienceValid(a schema.Audience) error {
	if !safeID.MatchString(a.ID) || strings.TrimSpace(a.Name) == "" {
		return fmt.Errorf("Mailchimp returned an invalid audience")
	}
	return nil
}
func (c *client) health_check(ctx context.Context, _ *HealthCheckIn) (*HealthCheckOut, error) {
	raw, e := c.request(ctx, "GET", "/3.0/ping", nil, nil, 200)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	var w struct {
		Status string `json:"health_status"`
	}
	if decode(raw, &w) != nil || w.Status != "Everything's Chimpy!" {
		return &HealthCheckOut{Message: "Mailchimp returned an unrecognized health response"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Mailchimp authenticated ping succeeded"}, nil
}
func (c *client) list_audiences(ctx context.Context, in *ListAudiencesIn) (*ListAudiencesOut, error) {
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 1000)
	if e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", "/3.0/lists", url.Values{"count": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "sort_field": {"date_created"}, "sort_dir": {"ASC"}}, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows  []schema.Audience `json:"lists"`
		Total *int              `json:"total_items"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Mailchimp returned invalid audiences")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	for _, a := range w.Rows {
		if e = audienceValid(a); e != nil {
			return nil, e
		}
	}
	return &ListAudiencesOut{Audiences: w.Rows, Total: *w.Total, HasMore: more, NextOffset: next}, nil
}
func (c *client) get_audience(ctx context.Context, in *GetAudienceIn) (*GetAudienceOut, error) {
	p, e := audiencePath(in.ListID)
	if e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", p, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	var a schema.Audience
	if decode(raw, &a) != nil || a.ID != in.ListID {
		return nil, fmt.Errorf("Mailchimp returned a mismatched audience")
	}
	if e = audienceValid(a); e != nil {
		return nil, e
	}
	return &GetAudienceOut{Audience: a}, nil
}
func (c *client) list_members(ctx context.Context, in *ListMembersIn) (*ListMembersOut, error) {
	p, e := audiencePath(in.ListID)
	if e != nil {
		return nil, e
	}
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 1000)
	if e != nil {
		return nil, e
	}
	if in.Status != "" && !validStatus(in.Status, true) {
		return nil, fmt.Errorf("invalid subscription status")
	}
	if e = timestamp(in.SinceLastChanged); e != nil {
		return nil, e
	}
	q := url.Values{"count": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "sort_field": {"last_changed"}, "sort_dir": {"ASC"}}
	if in.Status != "" {
		q.Set("status", in.Status)
	}
	if in.SinceLastChanged != "" {
		q.Set("since_last_changed", in.SinceLastChanged)
	}
	if in.Options.VipOnly != nil {
		q.Set("vip_only", strconv.FormatBool(*in.Options.VipOnly))
	}
	raw, e := c.request(ctx, "GET", p+"/members", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows  []schema.Member `json:"members"`
		Total *int            `json:"total_items"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Mailchimp returned invalid members")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	for i := range w.Rows {
		if e = memberValid(&w.Rows[i]); e != nil {
			return nil, e
		}
	}
	return &ListMembersOut{Members: w.Rows, Total: *w.Total, HasMore: more, NextOffset: next}, nil
}
func (c *client) get_member(ctx context.Context, in *GetMemberIn) (*GetMemberOut, error) {
	p, e := memberPath(in.ListID, in.EmailAddress)
	if e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", p, nil, nil, 200)
	if e != nil {
		return nil, e
	}
	m, e := readMember(raw, p, in.EmailAddress)
	if e != nil {
		return nil, e
	}
	return &GetMemberOut{Member: m}, nil
}
func readMember(raw []byte, path, address string) (schema.Member, error) {
	var m schema.Member
	if decode(raw, &m) != nil {
		return m, fmt.Errorf("Mailchimp returned invalid member data")
	}
	if e := memberValid(&m); e != nil {
		return m, e
	}
	if !strings.HasSuffix(path, "/"+m.ID) || !strings.EqualFold(m.EmailAddress, address) {
		return m, fmt.Errorf("Mailchimp returned a mismatched member")
	}
	return m, nil
}
func (c *client) upsert_member(ctx context.Context, in *UpsertMemberIn) (*UpsertMemberOut, error) {
	p, e := memberPath(in.ListID, in.EmailAddress)
	if e != nil {
		return nil, e
	}
	if !validStatus(in.StatusIfNew, false) {
		return nil, fmt.Errorf("explicit status_if_new is required")
	}
	body := map[string]any{"email_address": in.EmailAddress, "status_if_new": in.StatusIfNew}
	if in.MergeFields != nil {
		if e := mergeNumbers(in.MergeFields, 0); e != nil {
			return nil, e
		}
		for k := range in.MergeFields {
			if !regexp.MustCompile(`^[A-Za-z0-9_]{1,64}$`).MatchString(k) {
				return nil, fmt.Errorf("invalid merge tag")
			}
		}
		body["merge_fields"] = in.MergeFields
	}
	if in.Options.Vip != nil {
		body["vip"] = *in.Options.Vip
	}
	raw, e := c.request(ctx, "PUT", p, nil, body, 200)
	if e != nil {
		return nil, e
	}
	m, e := readMember(raw, p, in.EmailAddress)
	if e != nil {
		return nil, e
	}
	return &UpsertMemberOut{Member: m}, nil
}
func (c *client) list_member_tags(ctx context.Context, in *ListMemberTagsIn) (*ListMemberTagsOut, error) {
	p, e := memberPath(in.ListID, in.EmailAddress)
	if e != nil {
		return nil, e
	}
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 1000)
	if e != nil {
		return nil, e
	}
	raw, e := c.request(ctx, "GET", p+"/tags", url.Values{"count": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}}, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows []struct {
			ID   exactID `json:"id"`
			Name string  `json:"name"`
			Date string  `json:"date_added"`
		} `json:"tags"`
		Total *int `json:"total_items"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Mailchimp returned invalid tags")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	out := &ListMemberTagsOut{Tags: []schema.MemberTag{}, Total: *w.Total, HasMore: more, NextOffset: next}
	for _, t := range w.Rows {
		if t.ID == "" || t.Name == "" {
			return nil, fmt.Errorf("Mailchimp tag is missing its ID or name")
		}
		out.Tags = append(out.Tags, schema.MemberTag{ID: string(t.ID), Name: t.Name, DateAdded: t.Date})
	}
	return out, nil
}
func (c *client) list_campaigns(ctx context.Context, in *ListCampaignsIn) (*ListCampaignsOut, error) {
	limit, offset, e := pagination(in.Page.Limit, in.Page.Offset, 1000)
	if e != nil {
		return nil, e
	}
	q := url.Values{"count": {strconv.Itoa(limit)}, "offset": {strconv.Itoa(offset)}, "sort_field": {"create_time"}, "sort_dir": {"ASC"}}
	if in.ListID != "" {
		if _, e = audiencePath(in.ListID); e != nil {
			return nil, e
		}
		q.Set("list_id", in.ListID)
	}
	if in.Status != "" {
		switch in.Status {
		case "save", "paused", "schedule", "sending", "sent":
		default:
			return nil, fmt.Errorf("invalid campaign status")
		}
		q.Set("status", in.Status)
	}
	raw, e := c.request(ctx, "GET", "/3.0/campaigns", q, nil, 200)
	if e != nil {
		return nil, e
	}
	var w struct {
		Rows []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Status   string `json:"status"`
			Create   string `json:"create_time"`
			Send     string `json:"send_time"`
			Settings struct {
				Title   string `json:"title"`
				Subject string `json:"subject_line"`
			} `json:"settings"`
			Recipients struct {
				ListID string `json:"list_id"`
			} `json:"recipients"`
		} `json:"campaigns"`
		Total *int `json:"total_items"`
	}
	if decode(raw, &w) != nil || w.Rows == nil {
		return nil, fmt.Errorf("Mailchimp returned invalid campaigns")
	}
	more, next, e := pageResult(w.Total, len(w.Rows), offset, limit)
	if e != nil {
		return nil, e
	}
	out := &ListCampaignsOut{Campaigns: []schema.Campaign{}, Total: *w.Total, HasMore: more, NextOffset: next}
	for _, r := range w.Rows {
		if r.ID == "" || r.Type == "" || r.Status == "" {
			return nil, fmt.Errorf("Mailchimp campaign is missing its identity or state")
		}
		out.Campaigns = append(out.Campaigns, schema.Campaign{ID: r.ID, Type: r.Type, Status: r.Status, Title: r.Settings.Title, SubjectLine: r.Settings.Subject, ListID: r.Recipients.ListID, CreateTime: r.Create, SendTime: r.Send})
	}
	return out, nil
}

// Input JSON is bound by the SDK; reject unsafe float64 values before a write.
func mergeNumbers(v any, depth int) error {
	if depth > 16 {
		return fmt.Errorf("merge fields exceed the supported nesting depth")
	}
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) || math.Abs(x) > 9007199254740991 {
			return fmt.Errorf("numeric merge values must be finite and within the exact JSON integer range; use a text merge field for larger identifiers")
		}
	case map[string]any:
		for _, child := range x {
			if e := mergeNumbers(child, depth+1); e != nil {
				return e
			}
		}
	case []any:
		for _, child := range x {
			if e := mergeNumbers(child, depth+1); e != nil {
				return e
			}
		}
	}
	return nil
}
