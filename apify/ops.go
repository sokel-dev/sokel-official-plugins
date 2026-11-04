package main

import (
	"encoding/json"
	"errors"
	"github.com/sokel-dev/sokel-official-plugins/apify/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

func clientFor(ctx plugin.Ctx) (*apiClient, error) {
	return newAPIClient("SOKEL_APIFY_BASE_URL", "https://api.apify.com", sokel.CredentialAs[Cred](ctx).APIKey, true, 45*time.Second)
}
func (c *apiClient) apify(ctx plugin.Ctx, method, path string, body any, out any) error {
	var env struct {
		Data  json.RawMessage `json:"data"`
		Error json.RawMessage `json:"error"`
	}
	if e := c.request(ctx, method, path, body, nil, &env); e != nil {
		return e
	}
	if len(env.Error) > 0 && string(env.Error) != "null" {
		return errors.New("Apify rejected the request; check resource access and Actor input")
	}
	if len(env.Data) == 0 || string(env.Data) == "null" {
		return errors.New("Apify response is missing data")
	}
	if json.Unmarshal(env.Data, out) != nil {
		return errors.New("Apify returned an unexpected data shape")
	}
	return nil
}
func validateRun(r *schema.Run, key string) error {
	if validID(r.ID) != nil || r.ActorID == "" || r.Status == "" || r.StartedAt == "" {
		return errors.New("Apify returned an incomplete Actor run")
	}
	if r.StatusMessage != nil {
		s := strings.ReplaceAll(*r.StatusMessage, key, "[redacted]")
		r.StatusMessage = &s
	}
	return nil
}
func opRunActor(ctx plugin.Ctx, in *RunActorIn) (*RunActorOut, error) {
	if e := validID(in.ActorID); e != nil {
		return nil, e
	}
	if len(in.Build) > 128 || strings.ContainsAny(in.Build, "\r\n") {
		return nil, errors.New("build must be a single-line tag or number of at most 128 bytes")
	}
	if e := checkRange(in.Options.TimeoutSeconds, 0, 604800, "timeout_seconds"); e != nil {
		return nil, e
	}
	if e := checkRange(in.Options.MaxItems, 0, 1000000000, "max_items"); e != nil {
		return nil, e
	}
	if n := in.Options.MaxTotalChargeUSD; n != nil && (math.IsNaN(*n) || math.IsInf(*n, 0) || *n < 0) {
		return nil, errors.New("max_total_charge_usd must be a finite nonnegative number")
	}
	q := url.Values{"waitForFinish": {"0"}, "restartOnError": {"false"}}
	if in.Build != "" {
		q.Set("build", in.Build)
	}
	if p := in.Options.TimeoutSeconds; p != nil {
		q.Set("timeout", strconv.Itoa(*p))
	}
	if p := in.Options.MaxItems; p != nil {
		q.Set("maxItems", strconv.Itoa(*p))
	}
	if p := in.Options.MaxTotalChargeUSD; p != nil {
		q.Set("maxTotalChargeUsd", strconv.FormatFloat(*p, 'f', -1, 64))
	}
	input := in.Input
	if input == nil {
		input = map[string]any{}
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var run schema.Run
	if e = c.apify(ctx, http.MethodPost, "/v2/actors/"+url.PathEscape(in.ActorID)+"/runs?"+q.Encode(), input, &run); e != nil {
		return nil, e
	}
	if e = validateRun(&run, c.key); e != nil {
		return nil, e
	}
	return &RunActorOut{Run: run}, nil
}
func opGetRun(ctx plugin.Ctx, in *GetRunIn) (*GetRunOut, error) {
	if e := validID(in.RunID); e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var run schema.Run
	if e = c.apify(ctx, http.MethodGet, "/v2/actor-runs/"+url.PathEscape(in.RunID), nil, &run); e != nil {
		return nil, e
	}
	if e = validateRun(&run, c.key); e != nil {
		return nil, e
	}
	return &GetRunOut{Run: run}, nil
}
func page(p schema.PageOptions) (url.Values, int, int, error) {
	if e := checkRange(p.Offset, 0, 1000000000, "offset"); e != nil {
		return nil, 0, 0, e
	}
	if e := checkRange(p.Limit, 1, 1000, "limit"); e != nil {
		return nil, 0, 0, e
	}
	offset, limit := 0, 100
	if p.Offset != nil {
		offset = *p.Offset
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	q := url.Values{"offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(limit)}}
	if p.Desc != nil {
		v := "0"
		if *p.Desc {
			v = "1"
		}
		q.Set("desc", v)
	}
	return q, offset, limit, nil
}
func pagination(offset, count, total, limit, wantOffset, wantLimit, actualCount int) (int, bool, error) {
	if offset != wantOffset || limit < 1 || limit > wantLimit || count < 0 || count > limit || count != actualCount || total < 0 {
		return 0, false, errors.New("Apify returned inconsistent pagination metadata")
	}
	next := offset + count
	if count > 0 && next > total {
		return 0, false, errors.New("Apify pagination total is smaller than the returned page")
	}
	more := next < total
	if more && count == 0 {
		return 0, false, errors.New("Apify returned an empty page before the reported total; retry after the dataset stabilizes")
	}
	return next, more, nil
}
func opListRuns(ctx plugin.Ctx, in *ListRunsIn) (*ListRunsOut, error) {
	if e := validID(in.ActorID); e != nil {
		return nil, e
	}
	q, offset, limit, e := page(in.Page)
	if e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r struct {
		Items  []schema.Run `json:"items"`
		Offset *int         `json:"offset"`
		Limit  *int         `json:"limit"`
		Count  *int         `json:"count"`
		Total  *int         `json:"total"`
	}
	if e = c.apify(ctx, http.MethodGet, "/v2/actors/"+url.PathEscape(in.ActorID)+"/runs?"+q.Encode(), nil, &r); e != nil {
		return nil, e
	}
	if r.Items == nil || r.Offset == nil || r.Limit == nil || r.Count == nil || r.Total == nil {
		return nil, errors.New("Apify omitted run pagination fields")
	}
	next, more, e := pagination(*r.Offset, *r.Count, *r.Total, *r.Limit, offset, limit, len(r.Items))
	if e != nil {
		return nil, e
	}
	for i := range r.Items {
		if e = validateRun(&r.Items[i], c.key); e != nil {
			return nil, e
		}
	}
	return &ListRunsOut{Runs: r.Items, Offset: *r.Offset, Count: *r.Count, Total: *r.Total, NextOffset: next, HasMore: more}, nil
}
func opGetDataset(ctx plugin.Ctx, in *GetDatasetIn) (*GetDatasetOut, error) {
	if e := validID(in.DatasetID); e != nil {
		return nil, e
	}
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var r schema.Dataset
	if e = c.apify(ctx, http.MethodGet, "/v2/datasets/"+url.PathEscape(in.DatasetID), nil, &r); e != nil {
		return nil, e
	}
	if validID(r.ID) != nil || r.CreatedAt == "" || r.ModifiedAt == "" || r.ItemCount < 0 || r.CleanItemCount < 0 {
		return nil, errors.New("Apify returned incomplete dataset metadata")
	}
	return &GetDatasetOut{Dataset: r}, nil
}
func opListDatasetItems(ctx plugin.Ctx, in *ListDatasetItemsIn) (*ListDatasetItemsOut, error) {
	if e := validID(in.DatasetID); e != nil {
		return nil, e
	}
	q, offset, limit, e := page(in.Page)
	if e != nil {
		return nil, e
	}
	q.Set("format", "json")
	c, e := clientFor(ctx)
	if e != nil {
		return nil, e
	}
	var items []map[string]any
	var headers http.Header
	if e = c.requestWithHeaders(ctx, http.MethodGet, "/v2/datasets/"+url.PathEscape(in.DatasetID)+"/items?"+q.Encode(), nil, nil, &items, &headers); e != nil {
		return nil, e
	}
	if items == nil {
		return nil, errors.New("Apify returned no JSON dataset array")
	}
	for _, item := range items {
		if item == nil {
			return nil, errors.New("Dataset items must be JSON objects")
		}
	}
	values := make([]int, 4)
	for i, name := range []string{"Offset", "Count", "Total", "Limit"} {
		raw := headers.Get("X-Apify-Pagination-" + name)
		values[i], e = strconv.Atoi(raw)
		if e != nil {
			return nil, errors.New("Apify omitted valid dataset pagination headers")
		}
	}
	next, more, e := pagination(values[0], values[1], values[2], values[3], offset, limit, len(items))
	if e != nil {
		return nil, e
	}
	return &ListDatasetItemsOut{Items: items, Offset: values[0], Count: values[1], Total: values[2], NextOffset: next, HasMore: more}, nil
}
func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	c, e := clientFor(ctx)
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	c.http.Timeout = 20 * time.Second
	var user struct {
		ID string `json:"id"`
	}
	if e = c.apify(ctx, http.MethodGet, "/v2/users/me", nil, &user); e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	if validID(user.ID) != nil {
		return &HealthCheckOut{Message: "Apify omitted authenticated user identity; credentials were not verified"}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Apify accepted the token on a read-only identity endpoint; Actor-specific permissions and spending limits are not checked"}, nil
}
