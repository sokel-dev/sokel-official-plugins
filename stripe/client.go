package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"regexp"
)

const stripeVersion = "2026-10-28.endive"

var stripeID = regexp.MustCompile(`^[A-Za-z0-9_]{4,255}$`)

func validID(id, prefix string) error {
	if len(id) <= len(prefix) || !stripeID.MatchString(id) || id[:len(prefix)] != prefix {
		return errors.New("Invalid Stripe resource ID for this operation.")
	}
	return nil
}

type listEnvelope struct {
	Object  string            `json:"object"`
	Data    []json.RawMessage `json:"data"`
	HasMore *bool             `json:"has_more"`
}

func (c *client) list(ctx context.Context, path, prefix, cursor string, filters url.Values) ([]json.RawMessage, bool, string, error) {
	q := url.Values{"limit": {"100"}}
	if cursor != "" {
		if e := validID(cursor, prefix); e != nil {
			return nil, false, "", e
		}
		q.Set("starting_after", cursor)
	}
	for k, v := range filters {
		q[k] = v
	}
	var r listEnvelope
	if _, e := c.json(ctx, "GET", "/v1/"+path, q, nil, &r); e != nil {
		return nil, false, "", e
	}
	if r.Object != "list" || r.Data == nil || r.HasMore == nil {
		return nil, false, "", errors.New("Stripe returned an invalid list response.")
	}
	if *r.HasMore && len(r.Data) == 0 {
		return nil, false, "", errors.New("Stripe reported more results without a cursor record.")
	}
	next := ""
	seen := map[string]bool{}
	for _, raw := range r.Data {
		var row struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(raw, &row) != nil || validID(row.ID, prefix) != nil || seen[row.ID] {
			return nil, false, "", errors.New("Stripe list contains an invalid or duplicate record ID.")
		}
		seen[row.ID] = true
		next = row.ID
	}
	if !*r.HasMore {
		next = ""
	} else if next == cursor {
		return nil, false, "", errors.New("Stripe repeated the current pagination cursor.")
	}
	return r.Data, *r.HasMore, next, nil
}
func (c *client) listCustomers(ctx context.Context, in *ListCustomersIn) (*ListCustomersOut, error) {
	q := url.Values{}
	if len(in.Email) > 320 {
		return nil, errors.New("Email filter exceeds 320 bytes.")
	}
	if in.Email != "" {
		q.Set("email", in.Email)
	}
	rows, more, next, e := c.list(ctx, "customers", "cus_", in.StartingAfter, q)
	if e != nil {
		return nil, e
	}
	out := &ListCustomersOut{Customers: []Customer{}, HasMore: more, NextStartingAfter: next}
	for _, r := range rows {
		var v Customer
		if e = decodeJSON(r, &v); e != nil {
			return nil, e
		}
		out.Customers = append(out.Customers, v)
	}
	return out, nil
}
func customerFilter(id string) (url.Values, error) {
	q := url.Values{}
	if id != "" {
		if e := validID(id, "cus_"); e != nil {
			return nil, e
		}
		q.Set("customer", id)
	}
	return q, nil
}
func (c *client) listSubscriptions(ctx context.Context, in *ListSubscriptionsIn) (*ListSubscriptionsOut, error) {
	q, e := customerFilter(in.Customer)
	if e != nil {
		return nil, e
	}
	if in.Status != "" {
		switch in.Status {
		case "all", "active", "past_due", "unpaid", "canceled", "incomplete", "incomplete_expired", "trialing", "paused":
		default:
			return nil, errors.New("Unsupported subscription status.")
		}
		q.Set("status", in.Status)
	}
	rows, more, next, e := c.list(ctx, "subscriptions", "sub_", in.StartingAfter, q)
	if e != nil {
		return nil, e
	}
	out := &ListSubscriptionsOut{Subscriptions: []Subscription{}, HasMore: more, NextStartingAfter: next}
	for _, r := range rows {
		var v Subscription
		if e = decodeJSON(r, &v); e != nil {
			return nil, e
		}
		if v.Status == "" {
			return nil, errors.New("Stripe subscription is missing its status.")
		}
		out.Subscriptions = append(out.Subscriptions, v)
	}
	return out, nil
}
func (c *client) listPaymentIntents(ctx context.Context, in *ListPaymentIntentsIn) (*ListPaymentIntentsOut, error) {
	q, e := customerFilter(in.Customer)
	if e != nil {
		return nil, e
	}
	rows, more, next, e := c.list(ctx, "payment_intents", "pi_", in.StartingAfter, q)
	if e != nil {
		return nil, e
	}
	out := &ListPaymentIntentsOut{PaymentIntents: []PaymentIntent{}, HasMore: more, NextStartingAfter: next}
	for _, r := range rows {
		var v PaymentIntent
		if e = decodeJSON(r, &v); e != nil {
			return nil, e
		}
		if v.Status == "" || v.Currency == "" {
			return nil, errors.New("Stripe payment intent is missing its status or currency.")
		}
		out.PaymentIntents = append(out.PaymentIntents, v)
	}
	return out, nil
}
func (c *client) get(ctx context.Context, path, id, prefix string, out any) error {
	if e := validID(id, prefix); e != nil {
		return e
	}
	_, e := c.json(ctx, "GET", "/v1/"+path+"/"+id, nil, nil, out)
	return e
}
func (c *client) getCustomer(ctx context.Context, in *GetCustomerIn) (*GetCustomerOut, error) {
	var r Customer
	if e := c.get(ctx, "customers", in.ID, "cus_", &r); e != nil {
		return nil, e
	}
	if r.ID != in.ID {
		return nil, errors.New("Stripe returned a different customer ID.")
	}
	return &GetCustomerOut{Customer: r}, nil
}
func (c *client) getSubscription(ctx context.Context, in *GetSubscriptionIn) (*GetSubscriptionOut, error) {
	var r Subscription
	if e := c.get(ctx, "subscriptions", in.ID, "sub_", &r); e != nil {
		return nil, e
	}
	if r.ID != in.ID || r.Status == "" {
		return nil, errors.New("Stripe returned an invalid subscription.")
	}
	return &GetSubscriptionOut{Subscription: r}, nil
}
func (c *client) getPaymentIntent(ctx context.Context, in *GetPaymentIntentIn) (*GetPaymentIntentOut, error) {
	var r PaymentIntent
	if e := c.get(ctx, "payment_intents", in.ID, "pi_", &r); e != nil {
		return nil, e
	}
	if r.ID != in.ID || r.Status == "" || r.Currency == "" {
		return nil, errors.New("Stripe returned an invalid payment intent.")
	}
	return &GetPaymentIntentOut{PaymentIntent: r}, nil
}
func (c *client) health(ctx context.Context) (*HealthCheckOut, error) {
	var r listEnvelope
	_, e := c.json(ctx, "GET", "/v1/customers", url.Values{"limit": {"1"}}, nil, &r)
	if e == nil && (r.Object != "list" || r.Data == nil || r.HasMore == nil) {
		e = errors.New("Stripe returned an invalid customer-list response.")
	}
	if e != nil {
		return &HealthCheckOut{Message: e.Error()}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Customer read permission verified. Other read permissions and webhook signing are checked separately."}, nil
}
