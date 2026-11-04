package main

import (
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func opRead(ctx plugin.Ctx, in *ReadIn) (*ReadOut, error) {
	target, e := pageURL(in.URL)
	if e != nil {
		return nil, e
	}
	format := in.Format
	if format == "" {
		format = "markdown"
	}
	if format != "markdown" && format != "text" && format != "html" {
		return nil, errors.New("format must be markdown, text or html")
	}
	for _, s := range []string{in.TargetSelector, in.RemoveSelector} {
		if len(s) > 4096 || strings.ContainsAny(s, "\r\n") {
			return nil, errors.New("Selectors must be single-line values no longer than 4096 bytes")
		}
	}
	c, e := newAPIClient("SOKEL_JINA_READER_BASE_URL", "https://r.jina.ai", sokel.CredentialAs[Cred](ctx).APIKey, false, 150*time.Second)
	if e != nil {
		return nil, e
	}
	headers := map[string]string{"X-Respond-With": format}
	if in.TargetSelector != "" {
		headers["X-Target-Selector"] = in.TargetSelector
	}
	if in.RemoveSelector != "" {
		headers["X-Remove-Selector"] = in.RemoveSelector
	}
	if in.Options.NoCache != nil {
		headers["X-No-Cache"] = strconv.FormatBool(*in.Options.NoCache)
	}
	var r struct {
		Code int `json:"code"`
		Data *struct {
			URL           string         `json:"url"`
			FinalURL      string         `json:"finalUrl"`
			Title         string         `json:"title"`
			Description   string         `json:"description"`
			Content       *string        `json:"content"`
			PublishedTime string         `json:"publishedTime"`
			Metadata      map[string]any `json:"metadata"`
			Warning       string         `json:"warning"`
			HTTPStatus    int            `json:"httpStatus"`
		} `json:"data"`
	}
	if e = c.request(ctx, http.MethodPost, "/", struct {
		URL string `json:"url"`
	}{target}, headers, &r); e != nil {
		return nil, e
	}
	if r.Code != 200 || r.Data == nil {
		return nil, errors.New("Jina Reader did not return a successful page response")
	}
	if r.Data.Content == nil || r.Data.URL == "" {
		return nil, errors.New("Jina Reader response is missing content or source URL")
	}
	if r.Data.HTTPStatus >= 400 {
		return nil, errors.New("The target page returned an HTTP error; verify access to the page")
	}
	return &ReadOut{URL: r.Data.URL, FinalURL: r.Data.FinalURL, Title: r.Data.Title, Description: r.Data.Description, Content: *r.Data.Content, PublishedTime: r.Data.PublishedTime, Metadata: r.Data.Metadata, Warning: r.Data.Warning}, nil
}
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	key := sokel.CredentialAs[Cred](ctx).APIKey
	if strings.TrimSpace(key) == "" {
		return &HealthCheckOut{Message: "Anonymous Reader mode: no API key to verify; run read to check page access"}, nil
	}
	c, e := newAPIClient("SOKEL_JINA_API_BASE_URL", "https://api.jina.ai", key, true, 20*time.Second)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	// The official OpenAPI marks GET /v1/batches as HTTPBearer-authenticated and read-only.
	// Jobs are neither created nor exposed to the caller of health_check.
	var jobs []json.RawMessage
	if e = c.request(ctx, http.MethodGet, "/v1/batches?limit=1", nil, nil, &jobs); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if jobs == nil {
		return &HealthCheckOut{Message: "Jina returned an unexpected account response; credentials were not verified"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Jina accepted the API key on a read-only account endpoint; Reader quota is not checked"}, nil
}
