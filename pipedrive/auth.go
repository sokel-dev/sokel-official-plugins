package main

import (
	"errors"
	"net/http"
	"regexp"
)

var companyDomain = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

func (c *client) origin() (string, error) {
	if !companyDomain.MatchString(c.cred.CompanyDomain) {
		return "", errors.New("Company subdomain must be one lowercase DNS label, without a URL or dots.")
	}
	return "https://" + c.cred.CompanyDomain + ".pipedrive.com", nil
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.APIToken) {
		return "", errors.New("A valid Pipedrive API token is required.")
	}
	return c.cred.APIToken, nil
}
func (c *client) setHeaders(r *http.Request, a string) { r.Header.Set("x-api-token", a) }
