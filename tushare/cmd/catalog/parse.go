package main

// HTML parsing for the doc site. Regex instead of pulling in an HTML parser library: it only
// relies on this one site's two fixed structures (the nav's ul/li nesting, and the paragraph
// markers in front of the parameter tables), and when parsing doesn't match, it **gets nothing**
// rather than parsing into something else — the caller lists every page it had to skip, so
// endpoints never silently go missing.

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const docBaseURL = "https://tushare.pro/document/2"

var hc = &http.Client{Timeout: 30 * time.Second}

func getHTML(url string) (string, error) {
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * time.Second)
		}
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return "", err
		}
		// Gets blocked without a UA.
		req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; sokel-catalog/1.0)")
		res, err := hc.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if res.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %d", res.StatusCode)
			continue
		}
		return string(body), nil
	}
	return "", lastErr
}

// ===== Navigation tree =====

var (
	navTokenRe = regexp.MustCompile(`<ul\b|</ul>|<a href="/document/2\?doc_id=(\d+)"[^>]*>(.*?)</a>`)
	tagRe      = regexp.MustCompile(`<[^>]+>`)
)

// fetchIndex scrapes the navigation tree. Hierarchy is reconstructed from the nesting depth of
// <ul>, where a later item at the same depth replaces the previous one, so every link can compute
// its own category path.
func fetchIndex() ([]node, error) {
	page, err := getHTML(docBaseURL)
	if err != nil {
		return nil, fmt.Errorf("拉取文档导航失败: %w", err)
	}
	start := strings.Index(page, `id="jstree"`)
	if start < 0 {
		return nil, fmt.Errorf("导航结构变了：找不到 id=\"jstree\"")
	}
	nav := page[start:]
	if end := strings.Index(nav, "</nav>"); end > 0 {
		nav = nav[:end]
	}

	var (
		out   []node
		path  []string
		depth int
	)
	for _, m := range navTokenRe.FindAllStringSubmatch(nav, -1) {
		switch {
		case m[0] == "</ul>":
			depth--
			if len(path) > depth {
				path = path[:max(depth, 0)]
			}
		case strings.HasPrefix(m[0], "<ul"):
			depth++
		default:
			id, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			name := textOf(m[2])
			for len(path) > depth-1 {
				path = path[:len(path)-1]
			}
			path = append(path, name)
			out = append(out, node{DocID: id, Name: name, Path: strings.Join(path, "/")})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("导航里一个链接都没解出来（页面结构变了）")
	}
	return out, nil
}

// ===== Endpoint detail =====

var (
	apiNameRes = []*regexp.Regexp{
		regexp.MustCompile(`接口\s*[：:]\s*([a-zA-Z_][\w]*)`),
		regexp.MustCompile(`pro\.query\(\s*['"]([a-zA-Z_][\w]*)['"]`),
		regexp.MustCompile(`pro\.([a-zA-Z_][\w]*)\s*\(`),
	}
	h2Re       = regexp.MustCompile(`(?s)<h2[^>]*>(.*?)</h2>`)
	tableRe    = regexp.MustCompile(`(?s)<table[^>]*>(.*?)</table>`)
	trRe       = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe     = regexp.MustCompile(`(?s)<t[hd][^>]*>(.*?)</t[hd]>`)
	describeRe = regexp.MustCompile(`描述\s*[：:]\s*([^<]{2,300})`)
)

// These names are calls from sample code, not endpoint names.
var notAPINames = map[string]bool{"query": true, "pro_api": true, "api": true, "daily_basic_": true}

// fetchDetail parses one doc page. Returning (nil, reason, nil) means this page isn't an endpoint
// page (a category page / decommissioned).
func fetchDetail(n node) (*API, string, error) {
	page, err := getHTML(fmt.Sprintf("%s?doc_id=%d", docBaseURL, n.DocID))
	if err != nil {
		return nil, "", err
	}
	text := textOf(page)

	inputs := paramsAfter(page, "输入参数")
	outputs := paramsAfter(page, "输出参数")
	if len(inputs) == 0 && len(outputs) == 0 {
		return nil, "非接口页（没有输入/输出参数表）", nil
	}

	name := ""
	for _, re := range apiNameRes {
		for _, m := range re.FindAllStringSubmatch(text, -1) {
			if !notAPINames[m[1]] {
				name = m[1]
				break
			}
		}
		if name != "" {
			break
		}
	}
	if name == "" {
		return nil, "有参数表但认不出接口名", nil
	}

	api := &API{APIName: name, DocID: n.DocID, Category: n.Path, Inputs: inputs, Outputs: outputs}
	if m := h2Re.FindStringSubmatch(page); m != nil {
		api.Title = textOf(m[1])
	}
	if api.Title == "" {
		api.Title = n.Name
	}
	if m := describeRe.FindStringSubmatch(text); m != nil {
		api.Describe = strings.TrimSpace(m[1])
	}
	return api, "", nil
}

// paramsAfter takes the **first** table after the marker paragraph.
// This is the sole but critical difference from the old scraper: a table's identity is decided by
// the marker, not guessed from its header.
func paramsAfter(page, marker string) []Param {
	i := strings.Index(page, marker)
	if i < 0 {
		return nil
	}
	m := tableRe.FindStringSubmatch(page[i:])
	if m == nil {
		return nil
	}
	rows := trRe.FindAllStringSubmatch(m[1], -1)
	if len(rows) < 2 {
		return nil
	}

	// The header decides column order. For the same two tables, some TuShare pages have outputs
	// "name/type/default-shown/description", others are missing "default-shown" — looking columns
	// up by name handles both.
	head := cellsOf(rows[0][1])
	col := map[string]int{}
	for i, h := range head {
		col[h] = i
	}
	nameIdx, ok := col["名称"]
	if !ok {
		return nil
	}
	typeIdx, hasType := col["类型"]
	reqIdx, hasReq := col["必选"]
	showIdx, hasShow := col["默认显示"]
	descIdx, hasDesc := col["描述"]
	if !hasDesc {
		descIdx, hasDesc = col["说明"]
	}

	var out []Param
	for _, row := range rows[1:] {
		cells := cellsOf(row[1])
		if nameIdx >= len(cells) {
			continue
		}
		name := cells[nameIdx]
		if name == "" {
			continue
		}
		p := Param{Name: name}
		if hasType && typeIdx < len(cells) {
			p.Type = cells[typeIdx]
		}
		if hasReq && reqIdx < len(cells) {
			p.Required = strings.EqualFold(cells[reqIdx], "Y")
		}
		if hasShow && showIdx < len(cells) {
			p.DefaultShow = strings.EqualFold(cells[showIdx], "Y")
		}
		if hasDesc && descIdx < len(cells) {
			p.Desc = cells[descIdx]
		}
		out = append(out, p)
	}
	return out
}

func cellsOf(rowHTML string) []string {
	var out []string
	for _, m := range cellRe.FindAllStringSubmatch(rowHTML, -1) {
		out = append(out, textOf(m[1]))
	}
	return out
}

// textOf strips tags, unescapes entities, and collapses whitespace.
func textOf(s string) string {
	s = tagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.TrimSpace(strings.Join(strings.Fields(s), " "))
}
