package main

// 轮询事件源：Events API + 失败流水线 → 五类平台事件。
//
// GitLab 没有长轮询/推流，出站模式下只有轮询一条路（延迟 = 轮询间隔，60s）。
// 要零延迟用平台的 Webhook 触发器（GitLab 项目 Settings→Webhooks 指过来），两路互补。
//
// 覆盖范围 = 凭证里 watch_projects 点名的项目——盯全实例的轮询量不可控。
// 没填就不起源（普通操作照用）。
//
// 两个游标各管各的：
//   - Events API 按 after（天粒度）+ 已见事件 id 集合去重——它的时间过滤只有天粒度，
//     同一天内靠 id 集合挡重放；
//   - 失败流水线按 updated_after（RFC3339）+ 已见 pipeline id。
//
// 首轮只记位置不回溯（feed/x 同款判断：接一个老项目别把十年历史灌进来）。
// 事件重推交平台按 (pluginId, event, eventID) 去重兜底——进程重启后集合是空的，
// 会短暂重报，平台去重挡住。

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

const pollInterval = 60 * time.Second

// glGet 事件源侧的 GET（SourceCtx 与操作侧 Ctx 是两个类型，HTTP 层复用）。
// 只收 context.Context：它真正用到的就这一样。收 sokel.SourceCtx 那个具体结构体（SDK v0.7.0 前），
// 等于把「能不能被测试调用」绑死在传输层的类型上。
func glGet(ctx context.Context, cred Cred, path string, params url.Values) ([]byte, error) {
	token := strings.TrimSpace(cred.Token)
	if token == "" {
		return nil, fmt.Errorf("凭证缺访问令牌")
	}
	base := strings.TrimRight(strings.TrimSpace(cred.BaseURL), "/")
	if base == "" {
		base = "https://gitlab.com"
	}
	full := base + "/api/v4" + path
	if enc := params.Encode(); enc != "" {
		full += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("连接 GitLab 失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GitLab 返回 HTTP %d（%s）", resp.StatusCode, path)
	}
	b := make([]byte, 0, 1<<16)
	buf := make([]byte, 32<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		b = append(b, buf[:n]...)
		if rerr != nil || len(b) > 16<<20 {
			break
		}
	}
	return b, nil
}

// glEvent Events API 的一条事件。
type glEvent struct {
	ID         int64  `json:"id"`
	ActionName string `json:"action_name"`
	TargetType string `json:"target_type"`
	TargetIID  int64  `json:"target_iid"`
	TargetTit  string `json:"target_title"`
	CreatedAt  string `json:"created_at"`
	Author     struct {
		Username string `json:"username"`
	} `json:"author"`
	Note *struct {
		Body string `json:"body"`
		// 实测字段（对着真实 GitLab 的 /events 抓的）：
		//   noteable_iid  才是 Issue 编号——事件层的 target_iid 是**评论自己的 id**
		//   noteable_type 区分 Issue / MergeRequest（两者走同一种 Note 事件）
		//   system        改标签、指派这些动作也会生成一条 note
		NoteableIID  int    `json:"noteable_iid"`
		NoteableType string `json:"noteable_type"`
		System       bool   `json:"system"`
	} `json:"note"`
	PushData *struct {
		CommitCount int    `json:"commit_count"`
		Ref         string `json:"ref"`
		CommitTo    string `json:"commit_to"`
		CommitTitle string `json:"commit_title"`
	} `json:"push_data"`
}

func runEvents(ctx plugin.SourceCtx) error {
	cred := sokel.SourceCredentialAs[Cred](ctx)
	projects := splitProjects(cred.WatchProjects)
	if len(projects) == 0 {
		return fmt.Errorf("凭证没填「事件盯哪些项目」，事件源不启动（普通操作不受影响）")
	}

	seenEvents := map[int64]bool{}  // Events API 的事件 id
	seenPipelines := map[int]bool{} // 已报过的失败流水线
	pipeSince := time.Now().UTC()   // 失败流水线的时间游标
	eventDay := time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
	first := true

	// Issue 标签变更：Events API 根本报不出来（它只有 opened/closed 那几个 action），
	// 只能单独轮 issues 列表、在内存里比对上一轮的标签。
	// 这条路是给**没配 webhook 的部署**兜底的——而「先建 Issue 再打标签」恰恰是最常见的用法。
	issueLabels := map[string]map[int][]string{} // proj → iid → 上一轮的标签
	for _, p := range projects {
		issueLabels[p] = map[int][]string{}
	}

	ctx.ReportStatus("ok", fmt.Sprintf("轮询 %d 个项目（%s 间隔）", len(projects), pollInterval))
	tick := time.NewTicker(pollInterval)
	defer tick.Stop()
	for {
		for _, proj := range projects {
			pollProject(ctx, cred, proj, seenEvents, first)
			pollFailedPipelines(ctx, cred, proj, seenPipelines, pipeSince, first)
			pollIssueLabels(ctx, cred, proj, issueLabels[proj], first)
		}
		if first {
			first = false // 首轮只记位置：老项目的历史不回溯
		}
		// 集合防膨胀：轮询窗口外的老 id 不会再出现，够大就整体换新。
		if len(seenEvents) > 5000 {
			seenEvents = map[int64]bool{}
			eventDay = time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02")
		}
		_ = eventDay
		select {
		case <-ctx.Done():
			return nil
		case <-tick.C:
		}
	}
}

func splitProjects(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pollProject 拉一个项目最近的事件并触发。
func pollProject(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int64]bool, first bool) {
	q := url.Values{"per_page": {"50"}}
	// after 只有天粒度：取昨天起，同天重放靠 seen 集合挡。
	q.Set("after", time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"))
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/events", q)
	if err != nil {
		log.Printf("[gitlab] 拉 %s 事件失败: %v", proj, err)
		return
	}
	var evs []glEvent
	_ = json.Unmarshal(raw, &evs)
	for _, ev := range evs {
		if seen[ev.ID] {
			continue
		}
		seen[ev.ID] = true
		if first {
			continue // 首轮只记位置
		}
		triggerEvent(ctx, cred, proj, ev)
	}
}

// triggerEvent 一条 Events API 事件 → 对应的平台事件（认不出的类型跳过）。
func triggerEvent(ctx plugin.SourceCtx, cred Cred, proj string, ev glEvent) {
	eid := fmt.Sprintf("%d", ev.ID)
	var raw any
	b, _ := json.Marshal(ev)
	_ = json.Unmarshal(b, &raw)
	action := strings.ToLower(ev.ActionName)
	switch {
	case ev.PushData != nil && strings.HasPrefix(action, "pushed"):
		_ = TriggerCommitPushed(ctx, eid, &CommitPushedEvent{
			Project: proj, Ref: ev.PushData.Ref, CommitID: ev.PushData.CommitTo,
			CommitTitle: ev.PushData.CommitTitle, CommitCount: ev.PushData.CommitCount,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	case ev.TargetType == "MergeRequest" && action == "opened":
		_ = TriggerMrOpened(ctx, eid, &MrOpenedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	case ev.TargetType == "MergeRequest" && action == "accepted":
		// GitLab 的「合并」在 Events API 里是 accepted——别按直觉找 merged。
		_ = TriggerMrMerged(ctx, eid, &MrMergedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	// 评论：Events API 把它记成 target_type=Note、action_name="commented on"，
	// 正文在 note.body。只认挂在 Issue 上的（noteable_type 这层 Events API 不给，
	// 用 target_type 兜住 Note 之后再按 target_iid 关联 Issue——DiffNote 等代码行评论
	// 的 target_type 不是 Note，天然被挡在外面）。
	case ev.TargetType == "Note" && strings.HasPrefix(action, "commented"):
		n := ev.Note
		// 三道过滤,每一道都是实测对着真实 GitLab 的 /events 定下来的：
		//   system：改标签/指派也会生成 note——不挡住就是「动一下标签就去派活」,直接烧钱
		//   noteable_type：Issue 与 MR 的评论走同一种事件,不分就会回错地方
		//   noteable_iid：**事件层的 target_iid 是评论自己的 id,不是 Issue 编号**
		//                 （实测 target_iid=1970 而 Issue 是 #4）——用错就是回到别人的 Issue 上
		if n == nil || n.System || n.NoteableIID <= 0 || strings.TrimSpace(n.Body) == "" {
			return
		}
		switch n.NoteableType {
		case "Issue":
			_ = TriggerIssueCommented(ctx, eid, &IssueCommentedEvent{
				Project: proj, Iid: n.NoteableIID, Title: ev.TargetTit,
				Comment: n.Body, Author: ev.Author.Username,
			})
		case "MergeRequest":
			_ = TriggerMrCommented(ctx, eid, &MrCommentedEvent{
				Project: proj, Iid: n.NoteableIID, Title: ev.TargetTit,
				Comment: n.Body, Author: ev.Author.Username,
			})
		}
	case ev.TargetType == "Issue" && action == "opened":
		// Events API 只给标题，正文与标签得再取一次详情。多一次调用换「下游拿得到 Issue 内容」——
		// 值：新 Issue 是低频事件，而没有正文的事件对「照着 Issue 干活」这类下游基本没用。
		desc, labels, issueURL := issueDetail(ctx, cred, proj, int(ev.TargetIID))
		_ = TriggerIssueOpened(ctx, eid, &IssueOpenedEvent{
			Project: proj, Iid: int(ev.TargetIID), Title: ev.TargetTit,
			Description: desc, Labels: labels, URL: issueURL,
			Author: ev.Author.Username, Raw: raw, Source: "poll",
		})
	}
}

// pollFailedPipelines 失败流水线不在 Events API 里（GitLab 的空白），单独轮询。
func pollFailedPipelines(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int]bool, since time.Time, first bool) {
	q := url.Values{"status": {"failed"}, "per_page": {"20"},
		"updated_after": {since.Format(time.RFC3339)}}
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/pipelines", q)
	if err != nil {
		log.Printf("[gitlab] 拉 %s 失败流水线失败: %v", proj, err)
		return
	}
	var ps []struct {
		ID     int    `json:"id"`
		Ref    string `json:"ref"`
		SHA    string `json:"sha"`
		WebURL string `json:"web_url"`
	}
	_ = json.Unmarshal(raw, &ps)
	for _, p := range ps {
		if seen[p.ID] {
			continue
		}
		seen[p.ID] = true
		if first {
			continue
		}
		_ = TriggerPipelineFailed(ctx, fmt.Sprintf("pipe-%d", p.ID), &PipelineFailedEvent{
			Project: proj, PipelineID: p.ID, Ref: p.Ref, SHA: p.SHA, WebURL: p.WebURL,
		})
	}
}

// issueDetail 取 Issue 正文/标签/链接。取不到就返回空——**不能让事件本身发不出去**：
// 详情拉失败是次要故障，而丢掉整条「有新 Issue」才是真损失。
func issueDetail(ctx context.Context, cred Cred, proj string, iid int) (desc string, labels []string, webURL string) {
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/issues/"+strconv.Itoa(iid), nil)
	if err != nil {
		log.Printf("[gitlab] 取 %s#%d 详情失败（事件照发，正文为空）: %v", proj, iid, err)
		return "", nil, ""
	}
	var d struct {
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		WebURL      string   `json:"web_url"`
	}
	if json.Unmarshal(raw, &d) != nil {
		return "", nil, ""
	}
	return d.Description, d.Labels, d.WebURL
}

// pollIssueLabels 轮询 Issue 的标签变化。
//
// 为什么要单开一条：加标签在 GitLab 那边是 update，不是 open，issue_opened 不会响；
// 而 Events API 的 action_name 里也没有标签变更这一类。要在**没有 webhook** 的部署上
// 支持「先建 Issue 再打标签派活」，只能自己轮 + 自己比对。
//
// 首轮只记位置不触发（与其它源同约定）：接一个已经堆了几百个带标签 Issue 的项目，
// 不该把它们全当成「刚打上标签」灌成几百条运行。
func pollIssueLabels(ctx plugin.SourceCtx, cred Cred, proj string, seen map[int][]string, first bool) {
	q := url.Values{"per_page": {"50"}, "order_by": {"updated_at"}, "state": {"opened"}}
	raw, err := glGet(ctx, cred, "/projects/"+url.PathEscape(proj)+"/issues", q)
	if err != nil {
		log.Printf("[gitlab] 轮询 %s 的 Issue 标签失败: %v", proj, err)
		return
	}
	var issues []struct {
		IID         int      `json:"iid"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Labels      []string `json:"labels"`
		WebURL      string   `json:"web_url"`
		UpdatedAt   string   `json:"updated_at"`
	}
	if json.Unmarshal(raw, &issues) != nil {
		return
	}
	for _, is := range issues {
		prev, known := seen[is.IID]
		seen[is.IID] = is.Labels
		if first || !known {
			continue // 首轮 / 第一次见到这个 Issue：只记位置
		}
		had := map[string]bool{}
		for _, l := range prev {
			had[l] = true
		}
		var added []string
		for _, l := range is.Labels {
			if !had[l] {
				added = append(added, l)
			}
		}
		if len(added) == 0 {
			continue
		}
		sort.Strings(added)
		// event_id 带 updated_at：摘掉再打回同一个标签是两次真事件。
		_ = TriggerIssueLabeled(ctx, fmt.Sprintf("poll:issue-label:%s:%d:%s", proj, is.IID, is.UpdatedAt),
			&IssueLabeledEvent{
				Project: proj, Iid: is.IID, Title: is.Title, Description: is.Description,
				AddedLabels: added, Labels: is.Labels, URL: is.WebURL,
			})
	}
}
