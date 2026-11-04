package main

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// The deployment URL is trusted configuration; no URL can be supplied by an operation.
func (c *client) origin() (string, error) {
	u, e := url.Parse(c.baseURL)
	if e != nil || len(c.baseURL) > 4096 || u == nil || u.Opaque != "" || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.ContainsAny(c.baseURL, "\\\r\n\t ") || u.RawPath != "" || strings.Contains(u.Path, "%") {
		return "", errors.New("Set SOKEL_QDRANT_URL to an absolute HTTP or HTTPS service URL without credentials, query or fragment.")
	}
	if u.Port() != "" {
		p, e := strconv.Atoi(u.Port())
		if e != nil || p < 1 || p > 65535 {
			return "", errors.New("Invalid service URL port.")
		}
	}
	for _, part := range strings.Split(u.Path, "/") {
		if part == "." || part == ".." {
			return "", errors.New("Service URL path must not contain dot segments.")
		}
	}
	if strings.Contains(u.Path, "//") {
		return "", errors.New("Service URL path must not contain empty segments.")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
func (c *client) authorization() (string, error) {
	if c.cred.APIKey == "" {
		return "", nil
	}
	if !safeToken(c.cred.APIKey) {
		return "", errors.New("API key must contain only printable non-space ASCII characters.")
	}
	return c.cred.APIKey, nil
}
func (c *client) setHeaders(r *http.Request, key string) {
	if key != "" {
		r.Header.Set("api-key", key)
	}
}
