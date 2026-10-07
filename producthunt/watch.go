package main

// Event source: poll the watched products (listed in the credential, plus the account's own recent products) for new
// comments and replies.
//
// Same rules as the other polling sources: the first round pushes no history, the cursor lives in the credential,
// a failed push stops the round without moving the cursor (the platform deduplicates on the event id), the interval
// has a floor. The cursor is the newest createdAt pushed plus the ids pushed in a lookback window.

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const (
	defaultPollSeconds = 180
	minPollSeconds     = 60
	lookbackSeconds    = 300
	// maxPagesPerPost bounds a round on a post whose comments are pouring in: pages are newest first and stop as
	// soon as a page's oldest comment predates the cursor, so a quiet post costs one request.
	maxPagesPerPost = 3
)

type cursor struct {
	T    int64            `json:"t"`
	Seen map[string]int64 `json:"seen,omitempty"`
}

func parseTime(s string) int64 {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	return 0
}

func pollInterval(s string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		n = defaultPollSeconds
	}
	if n < minPollSeconds {
		n = minPollSeconds
	}
	return time.Duration(n) * time.Second
}

func runWatchSource(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	mine := !strings.EqualFold(strings.TrimSpace(cred.WatchMyPosts), "off")
	listed := slugList(cred.WatchPosts)
	if !mine && len(listed) == 0 {
		log.Printf("producthunt: 凭证没配监听，事件源空转")
		ctx.ReportStatus("running", "未开启监听")
		<-ctx.Done()
		return ctx.Err()
	}
	interval := pollInterval(cred.PollSeconds)
	var cur *cursor
	if cred.WatchCursor != "" {
		cur = &cursor{}
		if json.Unmarshal([]byte(cred.WatchCursor), cur) != nil {
			cur = nil
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		before, _ := json.Marshal(cur)
		var err error
		if cur == nil {
			cur = &cursor{T: time.Now().Unix(), Seen: map[string]int64{}} // first round: remember now, push nothing
		} else {
			err = pollRound(ctx, cred, mine, listed, cur)
		}
		if after, _ := json.Marshal(cur); string(after) != string(before) {
			if uerr := ctx.UpdateCredential(map[string]string{"watch_cursor": string(after)}); uerr != nil {
				log.Printf("producthunt: 游标写回凭证失败（重启后可能重推）: %v", uerr)
			}
		}
		if err != nil {
			log.Printf("producthunt: 轮询失败（%v），%s 后重试", err, interval)
			ctx.ReportStatus("error", err.Error())
		} else {
			ctx.ReportStatus("running", "")
		}
		t := time.NewTimer(interval)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		}
	}
}

func pollRound(ctx plugin.SourceCtx, cred Cred, mine bool, listed []string, cur *cursor) error {
	slugs := append([]string{}, listed...)
	if mine {
		var v viewerResp
		if err := gql(ctx, cred, qViewer, nil, &v); err != nil {
			return err
		}
		if v.Viewer != nil {
			for _, e := range v.Viewer.User.MadePosts.Edges {
				slugs = append(slugs, e.Node.Slug)
			}
		}
	}
	seenSlug := map[string]bool{}
	since := cur.T - lookbackSeconds
	for _, slug := range slugs {
		if seenSlug[slug] {
			continue
		}
		seenSlug[slug] = true
		// Pages are newest first except that the pinned first comment (the makers' launch comment) leads page one,
		// so the page's last edge, not its minimum, says whether older pages can still hold anything new.
		p, err := fetchComments(ctx, cred, slug, maxPagesPerPost*pageSize, func(page phPost) bool {
			n := len(page.Comments.Edges)
			return n > 0 && parseTime(page.Comments.Edges[n-1].Node.CreatedAt) < since
		})
		if err != nil {
			return err
		}
		all := flatComments(p)
		mineByID := map[string]bool{} // comment id -> written by the viewer, to tell replies to me
		for _, c := range all {
			mineByID[c.ID] = c.User.IsViewer
		}
		sort.SliceStable(all, func(i, j int) bool { return all[i].CreatedAt < all[j].CreatedAt })
		for _, c := range all {
			at := parseTime(c.CreatedAt)
			if at < since || c.User.IsViewer {
				continue
			}
			if _, ok := cur.Seen[c.ID]; ok {
				continue
			}
			sc := commentFrom(c)
			ev := CommentReceivedEvent{
				CommentID: c.ID, Body: c.Body, URL: c.URL, VotesCount: c.VotesCount, ParentID: c.ParentID,
				IsReplyToMe: c.ParentID != "" && mineByID[c.ParentID],
				Username:    sc.Username,
				PostID:      p.ID, PostName: p.Name, PostURL: p.URL, CreatedAt: c.CreatedAt,
			}
			if err := TriggerCommentReceived(ctx, "ph:comment:"+c.ID, &ev); err != nil {
				return err
			}
			if cur.Seen == nil {
				cur.Seen = map[string]int64{}
			}
			cur.Seen[c.ID] = at
			if at > cur.T {
				cur.T = at
			}
		}
	}
	for id, at := range cur.Seen {
		if at < cur.T-lookbackSeconds {
			delete(cur.Seen, id)
		}
	}
	return nil
}
