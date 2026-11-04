package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/twilio/schema"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var sidPattern = regexp.MustCompile(`^[A-Z]{2}[a-fA-F0-9]{32}$`)
var phonePattern = regexp.MustCompile(`^\+[1-9][0-9]{1,14}$`)
var alphaSender = regexp.MustCompile(`^[A-Za-z0-9 ]{1,11}$`)
var shortCode = regexp.MustCompile(`^[0-9]{5,6}$`)

func validSID(s string, prefixes ...string) error {
	if sidPattern.MatchString(s) {
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				return nil
			}
		}
	}
	return errors.New("Provide a Twilio SID with the correct resource prefix and 32 hexadecimal characters.")
}
func (c *client) authorization() (string, error) {
	if validSID(c.cred.AccountSid, "AC") != nil || !safeToken(c.cred.AuthToken) {
		return "", errors.New("Provide a valid Twilio Account SID and its Auth Token in credentials.")
	}
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(c.cred.AccountSid+":"+c.cred.AuthToken)), nil
}
func (c *client) accountPath() string { return "/2010-04-01/Accounts/" + c.cred.AccountSid }
func (c *client) getAccount(ctx context.Context, in *GetAccountIn) (*GetAccountOut, error) {
	var r schema.Account
	if e := c.json(ctx, "GET", c.accountPath()+".json", nil, nil, &r); e != nil {
		return nil, e
	}
	if r.SID != c.cred.AccountSid || r.Status == "" {
		return nil, errors.New("Twilio returned missing or mismatched account metadata.")
	}
	return &GetAccountOut{Account: r}, nil
}
func sender(s string) bool {
	return phonePattern.MatchString(s) || shortCode.MatchString(s) || (alphaSender.MatchString(s) && strings.ContainsAny(s, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"))
}
func messageForm(x schema.SendOptions) (url.Values, error) {
	if !phonePattern.MatchString(x.To) {
		return nil, errors.New("Recipient must be an E.164 phone number for SMS or MMS.")
	}
	if x.From == nil && x.MessagingServiceSID == nil {
		return nil, errors.New("Provide a sender or Messaging Service SID.")
	}
	q := url.Values{"To": {x.To}}
	if x.From != nil {
		if !sender(*x.From) {
			return nil, errors.New("Sender must be an E.164 number, short code or alphanumeric sender ID.")
		}
		q.Set("From", *x.From)
	}
	if x.MessagingServiceSID != nil {
		if e := validSID(*x.MessagingServiceSID, "MG"); e != nil {
			return nil, e
		}
		q.Set("MessagingServiceSid", *x.MessagingServiceSID)
	}
	body := ""
	if x.Body != nil {
		body = *x.Body
		if !utf8.ValidString(body) || strings.ContainsRune(body, 0) || utf8.RuneCountInString(body) > 1600 {
			return nil, errors.New("Message body must contain at most 1600 Unicode characters and no NUL characters.")
		}
		q.Set("Body", body)
	}
	if body == "" && len(x.MediaURLs) == 0 {
		return nil, errors.New("Provide a nonempty body or at least one media URL.")
	}
	if len(x.MediaURLs) > 10 {
		return nil, errors.New("At most 10 media URLs are supported per message.")
	}
	for _, s := range x.MediaURLs {
		u, e := url.Parse(s)
		if e != nil || len(s) > 4096 || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.ContainsAny(s, "\r\n\x00") {
			return nil, errors.New("Media URLs must be absolute HTTP or HTTPS URLs without credentials or fragments.")
		}
		q.Add("MediaUrl", s)
	}
	if x.SmartEncoded != nil {
		q.Set("SmartEncoded", strconv.FormatBool(*x.SmartEncoded))
	}
	if x.ValidityPeriod != nil {
		if *x.ValidityPeriod < 1 || *x.ValidityPeriod > 36000 {
			return nil, errors.New("Queue validity must be 1 to 36000 seconds.")
		}
		q.Set("ValidityPeriod", strconv.Itoa(*x.ValidityPeriod))
	}
	return q, nil
}
func (c *client) checkMessage(m schema.Message) error {
	if validSID(m.SID, "SM", "MM") != nil || m.AccountSID != c.cred.AccountSid || m.Status == "" {
		return errors.New("Twilio returned missing or mismatched message metadata.")
	}
	return nil
}
func (c *client) sendMessage(ctx context.Context, in *SendMessageIn) (*SendMessageOut, error) {
	q, e := messageForm(in.Message)
	if e != nil {
		return nil, e
	}
	var r schema.Message
	if e = c.json(ctx, "POST", c.accountPath()+"/Messages.json", nil, q, &r); e != nil {
		return nil, e
	}
	if e = c.checkMessage(r); e != nil {
		return nil, e
	}
	return &SendMessageOut{Message: r}, nil
}
func (c *client) getMessage(ctx context.Context, in *GetMessageIn) (*GetMessageOut, error) {
	if e := validSID(in.MessageSid, "SM", "MM"); e != nil {
		return nil, e
	}
	var r schema.Message
	if e := c.json(ctx, "GET", c.accountPath()+"/Messages/"+in.MessageSid+".json", nil, nil, &r); e != nil {
		return nil, e
	}
	if e := c.checkMessage(r); e != nil {
		return nil, e
	}
	if r.SID != in.MessageSid {
		return nil, errors.New("Twilio returned a different message.")
	}
	return &GetMessageOut{Message: r}, nil
}
func queryText(s string) bool {
	return s != "" && len(s) <= 1024 && utf8.ValidString(s) && !strings.ContainsAny(s, "\r\n\x00")
}
func validateQuery(path string, q url.Values) error {
	allowed := map[string]bool{"Page": true, "PageSize": true, "PageToken": true}
	if strings.HasSuffix(path, "/Messages.json") {
		for _, k := range []string{"To", "From", "DateSent", "DateSent<", "DateSent>"} {
			allowed[k] = true
		}
	}
	if strings.HasSuffix(path, "/IncomingPhoneNumbers.json") {
		for _, k := range []string{"FriendlyName", "PhoneNumber", "Beta", "Origin"} {
			allowed[k] = true
		}
	}
	if strings.HasSuffix(path, "/Media.json") {
		for _, k := range []string{"DateCreated", "DateCreated<", "DateCreated>"} {
			allowed[k] = true
		}
	}
	for k, vs := range q {
		if !allowed[k] || len(vs) != 1 || !queryText(vs[0]) {
			return errors.New("Twilio pagination contains unsupported or duplicate query parameters.")
		}
		v := vs[0]
		if k == "PageSize" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 1 || n > 1000 {
				return errors.New("Page size must be 1 to 1000.")
			}
		}
		if k == "Page" {
			n, e := strconv.Atoi(v)
			if e != nil || n < 0 {
				return errors.New("Twilio page number is invalid.")
			}
		}
		if strings.HasPrefix(k, "Date") {
			if _, e := time.Parse("2006-01-02", v); e != nil {
				return errors.New("Date filters must use YYYY-MM-DD in UTC.")
			}
		}
	}
	return nil
}
func continuation(path, s string) (url.Values, error) {
	u, e := url.Parse(s)
	if e != nil || len(s) > 8192 || u.IsAbs() || u.Host != "" || u.User != nil || u.Fragment != "" || u.Path != path || strings.ContainsAny(s, "\r\n\x00") {
		return nil, errors.New("Next page URI must refer to the same Twilio account and collection.")
	}
	q, e := url.ParseQuery(u.RawQuery)
	if e != nil {
		return nil, errors.New("Invalid Twilio pagination query.")
	}
	if e = validateQuery(path, q); e != nil {
		return nil, e
	}
	if q.Get("PageToken") == "" {
		return nil, errors.New("Twilio next page URI is missing its opaque page token.")
	}
	if q.Get("PageSize") == "" {
		q.Set("PageSize", "50")
	}
	return q, nil
}
func paging(path string, p schema.PageOptions) (url.Values, error) {
	if p.NextPageURI != nil {
		if p.Size != nil {
			return nil, errors.New("Use next page URI without a new page size or filters.")
		}
		return continuation(path, *p.NextPageURI)
	}
	size := 50
	if p.Size != nil {
		size = *p.Size
	}
	q := url.Values{"PageSize": {strconv.Itoa(size)}}
	return q, validateQuery(path, q)
}
func addFilters(q url.Values, f schema.MessageFilters) error {
	for k, v := range map[string]*string{"To": f.To, "From": f.From, "DateSent": f.DateSent, "DateSent>": f.SentAfter, "DateSent<": f.SentBefore} {
		if v != nil {
			q.Set(k, *v)
		}
	}
	if f.DateSent != nil && (f.SentAfter != nil || f.SentBefore != nil) {
		return errors.New("Use either an exact sent date or a sent-date range.")
	}
	if f.SentAfter != nil && f.SentBefore != nil && *f.SentAfter >= *f.SentBefore {
		return errors.New("Sent-after date must precede sent-before date.")
	}
	return validateQuery("/Messages.json", q)
}
func (c *client) collection(ctx context.Context, path string, q url.Values, key string) ([]json.RawMessage, string, error) {
	b, e := c.request(ctx, "GET", path, q, nil)
	if e != nil {
		return nil, "", e
	}
	var r map[string]json.RawMessage
	if e = decodeJSON(b, &r); e != nil {
		return nil, "", e
	}
	if r[key] == nil || string(r[key]) == "null" || r["next_page_uri"] == nil {
		return nil, "", errors.New("Twilio returned missing pagination metadata.")
	}
	var items []json.RawMessage
	if e = decodeJSON(r[key], &items); e != nil {
		return nil, "", e
	}
	limit, _ := strconv.Atoi(q.Get("PageSize"))
	if len(items) > limit {
		return nil, "", errors.New("Twilio returned more records than the requested page size.")
	}
	var next *string
	if e = decodeJSON(r["next_page_uri"], &next); e != nil {
		return nil, "", e
	}
	if next == nil || *next == "" {
		return items, "", nil
	}
	n, e := continuation(path, *next)
	if e != nil {
		return nil, "", e
	}
	if len(items) == 0 || n.Get("PageToken") == q.Get("PageToken") {
		return nil, "", errors.New("Twilio pagination did not advance.")
	}
	return items, path + "?" + n.Encode(), nil
}
func (c *client) listMessages(ctx context.Context, in *ListMessagesIn) (*ListMessagesOut, error) {
	path := c.accountPath() + "/Messages.json"
	q, e := paging(path, in.Page)
	if e != nil {
		return nil, e
	}
	f := in.Filters
	if in.Page.NextPageURI != nil && (f.To != nil || f.From != nil || f.DateSent != nil || f.SentAfter != nil || f.SentBefore != nil) {
		return nil, errors.New("Use next page URI without new filters.")
	}
	if in.Page.NextPageURI == nil {
		if e = addFilters(q, f); e != nil {
			return nil, e
		}
	}
	items, next, e := c.collection(ctx, path, q, "messages")
	if e != nil {
		return nil, e
	}
	out := &ListMessagesOut{Messages: []schema.Message{}, NextPageURI: next, HasMore: next != ""}
	for _, b := range items {
		var m schema.Message
		if e = decodeJSON(b, &m); e != nil {
			return nil, e
		}
		if e = c.checkMessage(m); e != nil {
			return nil, e
		}
		out.Messages = append(out.Messages, m)
	}
	return out, nil
}
func (c *client) checkNumber(n schema.IncomingNumber) error {
	if validSID(n.SID, "PN") != nil || n.AccountSID != c.cred.AccountSid || n.PhoneNumber == "" {
		return errors.New("Twilio returned missing or mismatched phone number metadata.")
	}
	return nil
}
func (c *client) getIncomingNumber(ctx context.Context, in *GetIncomingNumberIn) (*GetIncomingNumberOut, error) {
	if e := validSID(in.NumberSid, "PN"); e != nil {
		return nil, e
	}
	var n schema.IncomingNumber
	if e := c.json(ctx, "GET", c.accountPath()+"/IncomingPhoneNumbers/"+in.NumberSid+".json", nil, nil, &n); e != nil {
		return nil, e
	}
	if e := c.checkNumber(n); e != nil {
		return nil, e
	}
	if n.SID != in.NumberSid {
		return nil, errors.New("Twilio returned a different phone number.")
	}
	return &GetIncomingNumberOut{Number: n}, nil
}
func (c *client) listIncomingNumbers(ctx context.Context, in *ListIncomingNumbersIn) (*ListIncomingNumbersOut, error) {
	path := c.accountPath() + "/IncomingPhoneNumbers.json"
	q, e := paging(path, in.Page)
	if e != nil {
		return nil, e
	}
	items, next, e := c.collection(ctx, path, q, "incoming_phone_numbers")
	if e != nil {
		return nil, e
	}
	out := &ListIncomingNumbersOut{Numbers: []schema.IncomingNumber{}, NextPageURI: next, HasMore: next != ""}
	for _, b := range items {
		var n schema.IncomingNumber
		if e = decodeJSON(b, &n); e != nil {
			return nil, e
		}
		if e = c.checkNumber(n); e != nil {
			return nil, e
		}
		out.Numbers = append(out.Numbers, n)
	}
	return out, nil
}
func (c *client) listMessageMedia(ctx context.Context, in *ListMessageMediaIn) (*ListMessageMediaOut, error) {
	if e := validSID(in.MessageSid, "SM", "MM"); e != nil {
		return nil, e
	}
	path := c.accountPath() + "/Messages/" + in.MessageSid + "/Media.json"
	q, e := paging(path, in.Page)
	if e != nil {
		return nil, e
	}
	items, next, e := c.collection(ctx, path, q, "media_list")
	if e != nil {
		return nil, e
	}
	out := &ListMessageMediaOut{Media: []schema.Media{}, NextPageURI: next, HasMore: next != ""}
	for _, b := range items {
		var m schema.Media
		if e = decodeJSON(b, &m); e != nil {
			return nil, e
		}
		if validSID(m.SID, "ME") != nil || m.ParentSID != in.MessageSid || m.AccountSID != c.cred.AccountSid {
			return nil, errors.New("Twilio returned missing or mismatched media metadata.")
		}
		out.Media = append(out.Media, m)
	}
	return out, nil
}
