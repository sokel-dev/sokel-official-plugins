package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"os"
	"strconv"
	"time"
)

const stateKey = "_airtable_poll_state"
const maxWatchRecords = 500

type pollState struct {
	NextPollAt int64             `json:"next_poll_at,omitempty"`
	Base       string            `json:"base"`
	Table      string            `json:"table"`
	Ready      bool              `json:"ready"`
	Revision   uint64            `json:"revision"`
	Hashes     map[string]string `json:"hashes"`
}

func recordHash(r Record) string {
	b, _ := json.Marshal(r)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func pollOnce(ctx plugin.SourceCtx, c *client, base, table string, previous pollState) (pollState, error) {
	if _, e := tablePath(base, table); e != nil {
		return previous, e
	}
	if previous.Base != base || previous.Table != table {
		previous = pollState{Base: base, Table: table}
	}
	if previous.Revision == ^uint64(0) {
		return previous, errors.New("Polling revision counter is exhausted. Establish a new baseline.")
	}
	next := pollState{Base: base, Table: table, Ready: true, Revision: previous.Revision + 1, Hashes: map[string]string{}}
	var records []Record
	offset := ""
	seen := map[string]bool{}
	for page := 0; page < 6; page++ {
		r, e := c.records(ctx, &AirtableRecordsIn{BaseID: base, TableID: table, Offset: offset})
		if e != nil {
			return previous, e
		}
		for _, row := range r.Records {
			if _, exists := next.Hashes[row.ID]; exists {
				return previous, errors.New("Airtable pagination returned duplicate records. The baseline is not advanced.")
			}
			next.Hashes[row.ID] = recordHash(row)
			records = append(records, row)
		}
		if len(records) > maxWatchRecords {
			return previous, errors.New("Watched table exceeds 500 records. No events are emitted; use another synchronization method.")
		}
		if r.Offset == "" {
			break
		}
		if seen[r.Offset] || page == 5 {
			return previous, errors.New("Airtable pagination did not complete. The baseline is not advanced.")
		}
		seen[r.Offset] = true
		offset = r.Offset
		if err := wait(ctx, 250*time.Millisecond); err != nil {
			return previous, err
		}
	}
	if !previous.Ready {
		return next, nil
	}
	for _, r := range records {
		hash := next.Hashes[r.ID]
		old, exists := previous.Hashes[r.ID]
		if exists && old == hash {
			continue
		}
		change := "updated"
		if !exists {
			change = "created"
		}
		id := fmt.Sprintf("%s:%s:%s:%d:%s", base, table, r.ID, next.Revision, hash)
		if e := TriggerRecordChanged(ctx, id, &RecordChangedEvent{BaseID: base, TableID: table, Change: change, Record: r}); e != nil {
			return previous, e
		}
	}
	return next, nil
}
func pollInterval() time.Duration {
	n, e := strconv.Atoi(os.Getenv("AIRTABLE_POLL_SECONDS"))
	if e != nil || n < 60 || n > 86400 {
		n = 300
	}
	return time.Duration(n) * time.Second
}
func wait(ctx plugin.SourceCtx, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func runSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	if cred.WatchBase == "" && cred.WatchTable == "" {
		ctx.ReportStatus("running", "No watched table configured.")
		<-ctx.Done()
		return ctx.Err()
	}
	if cred.WatchBase == "" || cred.WatchTable == "" {
		return errors.New("Configure both a base ID and table ID for polling.")
	}
	var state pollState
	if raw := ctx.Credential()[stateKey]; raw != "" {
		if len(raw) > 128<<10 || json.Unmarshal([]byte(raw), &state) != nil {
			return errors.New("Invalid internal Airtable polling state.")
		}
		if len(state.Hashes) > maxWatchRecords {
			return errors.New("Internal Airtable polling state exceeds the record limit.")
		}
	}
	// Credential updates restart SDK sources. Carry the schedule across those restarts.
	if state.NextPollAt > 0 {
		delay := time.Until(time.UnixMilli(state.NextPollAt))
		if delay > 24*time.Hour {
			return errors.New("Internal polling schedule exceeds 24 hours.")
		}
		if delay > 0 {
			if err := wait(ctx, delay); err != nil {
				return err
			}
		}
	}
	c := newClient(cred.Token)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		next, err := pollOnce(ctx, c, cred.WatchBase, cred.WatchTable, state)
		delay := pollInterval()
		if err == nil {
			next.NextPollAt = time.Now().Add(delay).UnixMilli()
			b, _ := json.Marshal(next)
			if err = ctx.UpdateCredential(map[string]string{stateKey: string(b)}); err == nil {
				state = next
				ctx.ReportStatus("running", "")
			}
		}
		if err != nil {
			ctx.ReportStatus("error", err.Error())
			var rate *rateLimitError
			if errors.As(err, &rate) && rate.after > delay {
				delay = rate.after
			}
		}
		if err = wait(ctx, delay); err != nil {
			return err
		}
	}
}
