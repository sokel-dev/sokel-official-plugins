package main

// Incremental cursor. **This is the easiest part of this plugin to get wrong**, so it gets its
// own file to spell things out.
//
// Timestamp alone: a second item published in the same second gets missed, and wire-style
// feeds commonly emit several items per second. ID set alone: the set grows without bound, and
// the cursor eventually balloons to tens of KB stuffed into a data table row.
//
// So the cursor combines "timestamp + recently seen ids":
//   - newer than the timestamp -> keep;
//   - same timestamp but id not seen before -> also keep (this is how same-second items are handled);
//   - id already seen -> drop (this is how duplicate deliveries are handled).
// Only the most recent N seen ids are kept — items older than this window already have an
// older timestamp and won't collide anymore.

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-official-plugins/feed/schema"
)

// seenWindow is how many ids the cursor retains. 50 is enough to cover "a batch published at
// the same instant" without blowing up the cursor size (50 x 8-char hash ≈ 500 bytes).
const seenWindow = 50

// The cursor's **on-disk shape** is `timestamp|idhash,idhash,…`, e.g.
//
//	2026-08-19T08:35:52Z|2ceab3c4,6bb9e374,682f560a
//
// It used to just dump JSON, which was semantically fine, but it has to land in a data table
// cell as a **string** and get shown on the debug console — so the screen filled with `\"`,
// with about 30% of a 147-character string being escape characters. This shape is short, needs
// no escaping, and lets a human see at a glance "where it's stuck" when something goes wrong.
// Old cursors (JSON starting with `{`) are still recognized, otherwise a workflow already
// running would treat it as "no cursor" and start over from scratch.
type cursor struct {
	// T is the publish time of the newest item in this batch (RFC3339). If the source doesn't
	// provide a time, this is an empty string and dedup relies entirely on Seen.
	T string `json:"t,omitempty"`
	// Seen holds short hashes of recently seen item ids. We store the hash, not the raw value —
	// some sources use a whole URL as the id.
	Seen []string `json:"seen,omitempty"`
}

func parseCursor(raw string) cursor {
	s := strings.TrimSpace(raw)
	if s == "" {
		return cursor{}
	}
	if strings.HasPrefix(s, "{") { // old shape: a whole JSON blob
		var c cursor
		if json.Unmarshal([]byte(s), &c) != nil {
			// An unrecognizable cursor is treated as "no cursor": better to push a few
			// extra items this round than to get the whole stream stuck.
			return cursor{}
		}
		return c
	}
	t, seen, _ := strings.Cut(s, "|")
	c := cursor{T: strings.TrimSpace(t)}
	for _, h := range strings.Split(seen, ",") {
		if h = strings.TrimSpace(h); h != "" {
			c.Seen = append(c.Seen, h)
		}
	}
	return c
}

func (c cursor) dump() string {
	if c.T == "" && len(c.Seen) == 0 {
		return ""
	}
	return c.T + "|" + strings.Join(c.Seen, ",")
}

func (c cursor) hasSeen(id string) bool {
	h := shortHash(id)
	for _, s := range c.Seen {
		if s == h {
			return true
		}
	}
	return false
}

// filterNew picks out items newer than the cursor, returns them **in ascending time order**,
// and computes the next cursor.
//
// The first fetch (empty cursor) does **not** backfill the entire history: it just records the
// current position and returns the most recent batch — connecting a feed that's been publishing
// for ten years would otherwise flood the workflow with thousands of items at once (same
// reasoning as the x/notion event source).
func filterNew(items []schema.Item, cur cursor, max int, firstRun bool) ([]schema.Item, cursor, bool) {
	sortByTime(items)

	kept := make([]schema.Item, 0, len(items))
	for _, it := range items {
		if cur.hasSeen(it.ID) {
			continue
		}
		// **Only drop items strictly older than the cursor.** Items with the same
		// timestamp must not be dropped — wire-style feeds emit several items per second,
		// and the second item in the same second is deduped by the hasSeen check above,
		// not by the timestamp.
		if cur.T != "" && it.PublishedAt != "" && olderThan(it.PublishedAt, cur.T) {
			continue
		}
		kept = append(kept, it)
	}

	hasMore := false
	if max > 0 && len(kept) > max {
		// When over the limit, **keep the oldest batch**: the next round continues from
		// the cursor without skipping anything in between.
		kept = kept[:max]
		hasMore = true
	}
	if firstRun && len(kept) > 0 {
		// On the first run, only hand back the most recent batch, to avoid dumping ten
		// years of history in all at once.
		if max > 0 && len(kept) > max {
			kept = kept[len(kept)-max:]
		}
		hasMore = false
	}

	next := cur
	for _, it := range kept {
		if it.PublishedAt != "" && newerThan(it.PublishedAt, next.T) {
			next.T = it.PublishedAt
		}
		next.Seen = append(next.Seen, shortHash(it.ID))
	}
	if len(next.Seen) > seenWindow {
		next.Seen = next.Seen[len(next.Seen)-seenWindow:]
	}
	return kept, next, hasMore
}

// sortByTime sorts by publish time ascending. Items without a time are placed last, keeping
// their original relative order — most sources already deliver items newest-first, so reversing
// that order gives ascending order.
func sortByTime(items []schema.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].PublishedAt, items[j].PublishedAt
		if a == "" || b == "" {
			return b == "" && a != ""
		}
		return a < b
	})
}

// olderThan reports whether a is strictly earlier than b.
func olderThan(a, b string) bool {
	if b == "" {
		return false
	}
	ta, ea := time.Parse(time.RFC3339, a)
	tb, eb := time.Parse(time.RFC3339, b)
	if ea == nil && eb == nil {
		return ta.Before(tb)
	}
	return a < b
}

func newerThan(a, b string) bool {
	if b == "" {
		return true
	}
	ta, ea := time.Parse(time.RFC3339, a)
	tb, eb := time.Parse(time.RFC3339, b)
	if ea == nil && eb == nil {
		return ta.After(tb)
	}
	return a > b // if parsing fails, fall back to string comparison — RFC3339's lexical order matches time order
}

func shortHash(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:4])
}
