package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/apache-tika/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"mime"
	"net/http"
	"sort"
	"strings"
)

const maxMetadata = 16 << 20

func (c *client) parse(ctx plugin.Ctx, f *plugin.File, route, accept string) ([]byte, error) {
	b, e := readFile(ctx, f)
	if e != nil {
		return nil, e
	}
	ct := "application/octet-stream"
	if f.Mime != "" {
		typ, _, e := mime.ParseMediaType(f.Mime)
		if e != nil || !strings.Contains(typ, "/") {
			return nil, errors.New("Platform file has an invalid media type.")
		}
		ct = typ
	}
	h := http.Header{"Content-Type": {ct}, "Accept": {accept}, "Content-Disposition": {`attachment; filename="` + quotedFilename(f.Name) + `"`}}
	limit := int64(maxMetadata)
	if route == "/detect" {
		limit = 4096
	}
	out, headers, e := c.request(ctx, "PUT", route, b, h, limit)
	if e != nil {
		return nil, e
	}
	if e = mediaType(headers, accept); e != nil {
		return nil, e
	}
	return out, nil
}

// Tika metadata names depend on the detected document format. Values are strings
// or arrays; structured JSON values are retained as compact JSON strings.
func values(raw json.RawMessage) ([]string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, errors.New("Apache Tika returned an empty metadata value.")
	}
	if raw[0] == '[' {
		var items []json.RawMessage
		if decodeJSON(raw, &items) != nil {
			return nil, errors.New("Apache Tika returned invalid metadata values.")
		}
		out := make([]string, 0, len(items))
		for _, item := range items {
			s, e := scalar(item)
			if e != nil {
				return nil, e
			}
			out = append(out, s)
		}
		return out, nil
	}
	s, e := scalar(raw)
	if e != nil {
		return nil, e
	}
	return []string{s}, nil
}
func scalar(raw json.RawMessage) (string, error) {
	if len(raw) > 0 && raw[0] == '"' {
		var s string
		if decodeJSON(raw, &s) != nil {
			return "", errors.New("Apache Tika returned invalid text metadata.")
		}
		return s, nil
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return "", errors.New("Apache Tika returned invalid JSON metadata.")
	}
	return b.String(), nil
}
func documentMetadata(raw json.RawMessage) (schema.Document, error) {
	var obj map[string]json.RawMessage
	if e := decodeJSON(raw, &obj); e != nil {
		return schema.Document{}, e
	}
	if obj == nil || len(obj) == 0 {
		return schema.Document{}, errors.New("Apache Tika returned missing document metadata.")
	}
	var ct string
	if decodeJSON(obj["Content-Type"], &ct) != nil {
		return schema.Document{}, errors.New("Apache Tika returned metadata without a document media type.")
	}
	if typ, _, e := mime.ParseMediaType(ct); e != nil || !strings.Contains(typ, "/") {
		return schema.Document{}, errors.New("Apache Tika returned an invalid document media type.")
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := schema.Document{Metadata: []schema.MetadataField{}}
	for _, k := range keys {
		vs, e := values(obj[k])
		if e != nil {
			return schema.Document{}, e
		}
		if strings.HasPrefix(k, "tk:exception:") || strings.HasPrefix(k, "tk:warn:") {
			for _, v := range vs {
				if v != "" && v != "false" {
					return schema.Document{}, errors.New("Apache Tika reported a parsing exception, warning or truncated result. Inspect the service configuration and document; partial results were not returned.")
				}
			}
		}
		if strings.HasPrefix(k, "X-TIKA:") {
			return schema.Document{}, errors.New("This plugin requires Apache Tika 4.2-compatible metadata; older X-TIKA fields are not supported.")
		}
		if k == "tk:content" {
			if len(vs) != 1 || decodeJSON(obj[k], &out.Text) != nil {
				return schema.Document{}, errors.New("Apache Tika returned an invalid extracted-text field.")
			}
			continue
		}
		out.Metadata = append(out.Metadata, schema.MetadataField{Name: k, Values: vs})
	}
	return out, nil
}
func (c *client) extractText(ctx plugin.Ctx, in *ExtractTextIn) (*ExtractTextOut, error) {
	b, e := c.parse(ctx, in.File, "/tika/json/text", "application/json")
	if e != nil {
		return nil, e
	}
	d, e := documentMetadata(b)
	if e != nil {
		return nil, e
	}
	return &ExtractTextOut{Text: d.Text, Metadata: d.Metadata}, nil
}
func (c *client) extractMetadata(ctx plugin.Ctx, in *ExtractMetadataIn) (*ExtractMetadataOut, error) {
	b, e := c.parse(ctx, in.File, "/meta", "application/json")
	if e != nil {
		return nil, e
	}
	d, e := documentMetadata(b)
	if e != nil {
		return nil, e
	}
	return &ExtractMetadataOut{Metadata: d.Metadata}, nil
}
func (c *client) extractRecursive(ctx plugin.Ctx, in *ExtractRecursiveIn) (*ExtractRecursiveOut, error) {
	b, e := c.parse(ctx, in.File, "/rmeta/text", "application/json")
	if e != nil {
		return nil, e
	}
	var raw []json.RawMessage
	if e = decodeJSON(b, &raw); e != nil {
		return nil, e
	}
	if len(raw) < 1 || len(raw) > 10000 {
		return nil, errors.New("Apache Tika returned an empty result or more than 10000 document entries.")
	}
	out := &ExtractRecursiveOut{Documents: make([]schema.Document, 0, len(raw))}
	for _, r := range raw {
		d, e := documentMetadata(r)
		if e != nil {
			return nil, e
		}
		out.Documents = append(out.Documents, d)
	}
	return out, nil
}
func (c *client) detectType(ctx plugin.Ctx, in *DetectTypeIn) (*DetectTypeOut, error) {
	b, e := c.parse(ctx, in.File, "/detect", "text/plain")
	if e != nil {
		return nil, e
	}
	value := strings.TrimSpace(string(b))
	typ, params, e := mime.ParseMediaType(value)
	if e != nil || !strings.Contains(typ, "/") || len(params) != 0 || !safeText(value, 255) {
		return nil, errors.New("Apache Tika returned an invalid detected media type.")
	}
	return &DetectTypeOut{MediaType: typ}, nil
}
