package main

import (
	"errors"
	"net/http"
)

const apiVersion = "2.15"

func (c *client) origin() (string, error) {
	switch c.cred.Region {
	case "", "us":
		return "https://api.intercom.io", nil
	case "eu":
		return "https://api.eu.intercom.io", nil
	case "au":
		return "https://api.au.intercom.io", nil
	}
	return "", errors.New("Workspace region must be us, eu or au.")
}
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.AccessToken) {
		return "", errors.New("A valid Intercom access token is required.")
	}
	return "Bearer " + c.cred.AccessToken, nil
}
func (c *client) setHeaders(r *http.Request, a string) {
	r.Header.Set("Authorization", a)
	r.Header.Set("Intercom-Version", apiVersion)
}
