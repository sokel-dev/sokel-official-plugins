package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/nextcloud/schema"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

const propfindBody = `<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><d:resourcetype/><d:getcontentlength/><d:getcontenttype/><d:getlastmodified/><d:getetag/><oc:fileid/></d:prop></d:propfind>`

type resourceType struct {
	Collection *struct{} `xml:"DAV: collection"`
}
type davProps struct {
	Type        *resourceType `xml:"DAV: resourcetype"`
	Size        *string       `xml:"DAV: getcontentlength"`
	ContentType *string       `xml:"DAV: getcontenttype"`
	Modified    *string       `xml:"DAV: getlastmodified"`
	ETag        *string       `xml:"DAV: getetag"`
	FileID      *string       `xml:"http://owncloud.org/ns fileid"`
}
type davResponse struct {
	Href   string `xml:"DAV: href"`
	Status string `xml:"DAV: status"`
	Props  []struct {
		Prop   davProps `xml:"DAV: prop"`
		Status string   `xml:"DAV: status"`
	} `xml:"DAV: propstat"`
}
type multiStatus struct {
	XMLName   xml.Name      `xml:"DAV: multistatus"`
	Responses []davResponse `xml:"DAV: response"`
}

func statusCode(s string) int {
	p := strings.Fields(s)
	if len(p) < 2 || !strings.HasPrefix(p[0], "HTTP/") {
		return 0
	}
	n, _ := strconv.Atoi(p[1])
	return n
}
func (c *client) hrefPath(href string) (string, error) {
	root, e := c.endpoint("")
	if e != nil {
		return "", e
	}
	u, e := url.Parse(href)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", errors.New("invalid WebDAV resource URL")
	}
	if u.IsAbs() {
		if !strings.EqualFold(u.Scheme, root.Scheme) || !strings.EqualFold(u.Host, root.Host) {
			return "", errors.New("WebDAV returned a resource outside the configured origin")
		}
	} else if u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return "", errors.New("WebDAV href must be an absolute path or a same-origin URL")
	}
	escaped := strings.ToLower(u.EscapedPath())
	if strings.Contains(escaped, "%2f") || strings.Contains(escaped, "%5c") {
		return "", errors.New("WebDAV returned encoded path separators")
	}
	rp := strings.TrimSuffix(root.Path, "/")
	if u.Path == rp {
		return "", nil
	}
	if !strings.HasPrefix(u.Path, rp+"/") {
		return "", errors.New("WebDAV returned a resource outside the user's files")
	}
	return safePath(strings.TrimPrefix(u.Path, rp+"/"), true)
}
func (c *client) properties(ctx context.Context, p string, depth string) ([]schema.Resource, error) {
	b, _, _, e := c.request(ctx, "PROPFIND", p, []byte(propfindBody), http.Header{"Depth": {depth}, "Content-Type": {"application/xml; charset=utf-8"}}, maxXML, 207)
	if e != nil {
		return nil, e
	}
	var m multiStatus
	d := xml.NewDecoder(bytes.NewReader(b))
	if e = d.Decode(&m); e != nil {
		return nil, errors.New("invalid WebDAV multistatus XML")
	}
	for {
		tok, e := d.Token()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, errors.New("invalid trailing WebDAV XML")
		}
		if data, ok := tok.(xml.CharData); !ok || len(bytes.TrimSpace(data)) != 0 {
			return nil, errors.New("unexpected trailing WebDAV XML")
		}
	}
	if len(m.Responses) == 0 || len(m.Responses) > 2001 {
		return nil, errors.New("directory must contain at most 2000 children; no partial result is returned")
	}
	out := make([]schema.Resource, 0, len(m.Responses))
	seen := map[string]bool{}
	for _, r := range m.Responses {
		rp, e := c.hrefPath(r.Href)
		if e != nil {
			return nil, e
		}
		if seen[rp] {
			return nil, errors.New("duplicate WebDAV resource")
		}
		seen[rp] = true
		if r.Status != "" && statusCode(r.Status) != 200 {
			return nil, errors.New("WebDAV resource status was not successful")
		}
		resource := schema.Resource{Path: rp, Name: path.Base(rp)}
		if rp == "" {
			resource.Name = ""
		}
		hasType := false
		for _, ps := range r.Props {
			if statusCode(ps.Status) != 200 {
				continue
			}
			v := ps.Prop
			if v.Type != nil {
				hasType = true
				resource.IsDirectory = v.Type.Collection != nil
			}
			if v.Size != nil {
				s, e := strconv.ParseInt(*v.Size, 10, 64)
				if e != nil || s < 0 {
					return nil, errors.New("invalid WebDAV resource size")
				}
				resource.Size = &s
			}
			if v.FileID != nil {
				resource.FileID = v.FileID
			}
			if v.ETag != nil {
				resource.ETag = v.ETag
			}
			if v.ContentType != nil {
				resource.ContentType = v.ContentType
			}
			if v.Modified != nil {
				resource.ModifiedAt = v.Modified
			}
		}
		if !hasType {
			return nil, errors.New("WebDAV did not return a successful resource type property")
		}
		if rp != p && (depth != "1" || !directChild(p, rp)) {
			return nil, errors.New("WebDAV returned a resource outside the requested directory depth")
		}
		out = append(out, resource)
	}
	return out, nil
}
func directChild(parent, child string) bool {
	if child == "" {
		return false
	}
	if parent == "" {
		return !strings.Contains(child, "/")
	}
	return strings.HasPrefix(child, parent+"/") && !strings.Contains(strings.TrimPrefix(child, parent+"/"), "/")
}
func (c *client) getInfo(ctx context.Context, in *GetInfoIn) (*GetInfoOut, error) {
	p, e := safePath(in.Path, true)
	if e != nil {
		return nil, e
	}
	rs, e := c.properties(ctx, p, "0")
	if e != nil {
		return nil, e
	}
	if len(rs) != 1 || rs[0].Path != p {
		return nil, errors.New("WebDAV response does not identify the requested resource")
	}
	return &GetInfoOut{Resource: rs[0]}, nil
}
func (c *client) listDirectory(ctx context.Context, in *ListDirectoryIn) (*ListDirectoryOut, error) {
	p, e := safePath(in.Path, true)
	if e != nil {
		return nil, e
	}
	rs, e := c.properties(ctx, p, "1")
	if e != nil {
		return nil, e
	}
	out := &ListDirectoryOut{Children: []schema.Resource{}}
	found := false
	for _, r := range rs {
		if r.Path == p {
			out.Directory = r
			found = true
		} else {
			out.Children = append(out.Children, r)
		}
	}
	if !found || !out.Directory.IsDirectory {
		return nil, errors.New("requested resource is not a directory")
	}
	return out, nil
}
