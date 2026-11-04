package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const stateKey = "_slack_poll_state"

type pollState struct {
	NextPollAt int64  `json:"next_poll_at,omitempty"`
	Channel    string `json:"channel"`
	Oldest     string `json:"oldest"`
	Latest     string `json:"latest,omitempty"`
	Cursor     string `json:"cursor,omitempty"`
	MaxTS      string `json:"max_ts,omitempty"`
}

func compareTS(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	ai, _ := strconv.ParseInt(as[0], 10, 64)
	bi, _ := strconv.ParseInt(bs[0], 10, 64)
	if ai < bi {
		return -1
	}
	if ai > bi {
		return 1
	}
	return strings.Compare(as[1], bs[1])
}
func nowTS(now time.Time) string {
	return fmt.Sprintf("%d.%06d", now.Unix(), now.Nanosecond()/1000)
}
func pollOnce(ctx plugin.SourceCtx, c *client, channel string, state pollState, now time.Time) (pollState, error) {
	if err := validChannel(channel); err != nil {
		return state, err
	}
	if state.Channel != channel {
		state = pollState{Channel: channel}
	}
	if state.Oldest == "" {
		r, e := c.history(ctx, &SlackHistoryIn{Channel: channel})
		if e != nil {
			return state, e
		}
		state.Oldest = nowTS(now)
		if len(r.Messages) > 0 {
			state.Oldest = r.Messages[0].Ts
			for _, m := range r.Messages {
				if compareTS(m.Ts, state.Oldest) > 0 {
					state.Oldest = m.Ts
				}
			}
		}
		return state, nil
	}
	for _, v := range []string{state.Oldest, state.Latest, state.MaxTS} {
		if validTS(v, false) != nil {
			return state, errors.New("Invalid watch cursor. Clear the internal polling state to establish a new baseline.")
		}
	}
	previous := state
	if state.Latest == "" {
		state.Latest = nowTS(now)
		state.MaxTS = state.Oldest
	}
	if compareTS(state.Latest, state.Oldest) <= 0 {
		return previous, nil
	}
	r, e := c.history(ctx, &SlackHistoryIn{Channel: channel, Oldest: state.Oldest, Latest: state.Latest, Cursor: state.Cursor})
	if e != nil {
		return previous, e
	}
	if r.HasMore && r.NextCursor == "" {
		return previous, errors.New("Slack reports more messages without a pagination cursor. The current checkpoint is preserved.")
	}
	if r.NextCursor != "" && r.NextCursor == state.Cursor {
		return previous, errors.New("Slack returned a repeated pagination cursor. The current checkpoint is preserved.")
	}
	sort.Slice(r.Messages, func(i, j int) bool { return compareTS(r.Messages[i].Ts, r.Messages[j].Ts) < 0 })
	for _, m := range r.Messages {
		if compareTS(m.Ts, state.Oldest) <= 0 || compareTS(m.Ts, state.Latest) > 0 {
			continue
		}
		if compareTS(m.Ts, state.MaxTS) > 0 {
			state.MaxTS = m.Ts
		}
		if m.Type != "message" || m.Subtype != "" || m.BotID != "" || m.User == "" || (m.ThreadTs != "" && m.ThreadTs != m.Ts) {
			continue
		}
		if e := TriggerMessageReceived(ctx, channel+":"+m.Ts, &MessageReceivedEvent{Channel: channel, Ts: m.Ts, Text: m.Text, User: m.User}); e != nil {
			return previous, e
		}
	}
	if r.NextCursor != "" {
		state.Cursor = r.NextCursor
	} else {
		state.Oldest = state.MaxTS
		state.Cursor = ""
		state.Latest = ""
		state.MaxTS = ""
	}
	return state, nil
}
func pollInterval() time.Duration {
	n, e := strconv.Atoi(os.Getenv("SLACK_POLL_SECONDS"))
	if e != nil || n < 60 || n > 86400 {
		n = 60
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
	if cred.WatchChannel == "" {
		ctx.ReportStatus("running", "No watched channel configured.")
		<-ctx.Done()
		return ctx.Err()
	}
	state := pollState{}
	if raw := ctx.Credential()[stateKey]; raw != "" {
		if len(raw) > 16384 || json.Unmarshal([]byte(raw), &state) != nil {
			return errors.New("Invalid internal Slack polling state.")
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
		next, err := pollOnce(ctx, c, cred.WatchChannel, state, time.Now())
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
