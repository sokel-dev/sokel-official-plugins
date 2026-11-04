package main

import (
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"time"
)

// Advance locally after all event and checkpoint publications succeed. SDK v0.7.7
// does not acknowledge platform delivery or durable credential storage.
func sourceStep(ctx plugin.SourceCtx, a *api, cursor string) (string, error) {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	next, e := a.poll(ctx, cred, cursor, func(id string, event *RowAddedEvent) error { return TriggerRowAdded(ctx, id, event) })
	if e != nil {
		return cursor, e
	}
	if e = ctx.UpdateCredential(map[string]string{
		"poll_cursor":  next,
		"next_poll_at": time.Now().Add(pollInterval).UTC().Format(time.RFC3339Nano),
	}); e != nil {
		return cursor, fmt.Errorf("failed to publish polling checkpoint; the next attempt may replay events")
	}
	return next, nil
}

const pollInterval = 60 * time.Second

// Credential patches restart SDK sources. Carry the next deadline with the cursor
// so the replacement source waits instead of immediately issuing another request.
func pollDelay(raw string, now time.Time) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	next, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return 0, fmt.Errorf("invalid next_poll_at; clear this managed field to resume polling")
	}
	delay := next.Sub(now)
	if delay < 0 {
		return 0, nil
	}
	// Bound clock skew or an accidentally edited future timestamp to one interval.
	if delay > pollInterval {
		delay = pollInterval
	}
	return delay, nil
}
func waitForPoll(ctx plugin.SourceCtx, delay time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}
func runSource(ctx plugin.SourceCtx) error { return runSourceUsing(ctx, newAPI) }
func runSourceUsing(ctx plugin.SourceCtx, client func(string) *api) error {
	initial := sokel.SourceCredentialAs[Cred](ctx)
	cursor := initial.PollCursor
	delay, err := pollDelay(initial.NextPollAt, time.Now())
	if err != nil {
		return err
	}
	for {
		if err := waitForPoll(ctx, delay); err != nil {
			return err
		}
		cred := sokel.SourceCredentialAs[Cred](ctx)
		if cred.WatchSpreadsheetID != "" && cred.WatchRange != "" {
			next, e := sourceStep(ctx, client(cred.AccessToken), cursor)
			if e != nil {
				ctx.ReportStatus("error", e.Error())
			} else {
				cursor = next
				ctx.ReportStatus("running", "")
			}
		} else {
			ctx.ReportStatus("running", "Polling is disabled; configure the watch fields to enable it")
		}
		delay = pollInterval
	}
}
