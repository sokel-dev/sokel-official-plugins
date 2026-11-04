package main

import (
	"errors"
	"net/http"
)

const apiVersion = "2026-10"

func (c *client) origin() (string, error) { return "https://api.monday.com", nil }
func (c *client) authorization() (string, error) {
	if !safeToken(c.cred.AccessToken) {
		return "", errors.New("A valid Monday API token is required.")
	}
	return c.cred.AccessToken, nil
}
func (c *client) setHeaders(r *http.Request, a string) {
	r.Header.Set("Authorization", a)
	r.Header.Set("API-Version", apiVersion)
}
