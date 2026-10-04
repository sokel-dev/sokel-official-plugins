package main

// 嵌入：图片与链接卡片。
//
// 两者都要先把字节传成 blob（com.atproto.repo.uploadBlob），再把返回的 blob 引用塞进记录。
// **blob 不被记录引用就会被回收**，所以传完必须马上发帖——中间隔一个人工节点等半天是不行的，
// 这也是图片不单列成一个「上传」操作的原因。

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const (
	maxImages   = 4
	maxImgBytes = 2_000_000 // Bluesky 单张上限，超了回 BlobTooLarge
)

// blobRef：uploadBlob 的返回，原样塞回记录里即可。
type blobRef map[string]any

type imageItem struct {
	Alt   string  `json:"alt"`
	Image blobRef `json:"image"`
}

type imagesEmbedRec struct {
	Type   string      `json:"$type"`
	Images []imageItem `json:"images"`
}

type externalEmbedRec struct {
	Type     string `json:"$type"`
	External struct {
		URI         string  `json:"uri"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Thumb       blobRef `json:"thumb,omitempty"`
	} `json:"external"`
}

func uploadBlob(ctx plugin.Ctx, data []byte, mime string) (blobRef, error) {
	s, hc, cred, err := sessionFor(ctx)
	if err != nil {
		return nil, err
	}
	var out struct {
		Blob blobRef `json:"blob"`
	}
	// uploadBlob 收的是原始字节 + 真实 Content-Type（不是 JSON）。
	err = rpc(withBlobMime(ctx, mime), hc, pdsOf(cred), s.AccessJwt,
		http.MethodPost, "com.atproto.repo.uploadBlob", nil, data, &out)
	if err != nil {
		return nil, err
	}
	if out.Blob == nil {
		return nil, fmt.Errorf("Bluesky 没返回 blob 引用")
	}
	return out.Blob, nil
}

func imagesEmbed(ctx plugin.Ctx, files []*plugin.File, alts []string) (*imagesEmbedRec, error) {
	if len(files) > maxImages {
		return nil, fmt.Errorf("一条帖子最多 %d 张图，给了 %d 张", maxImages, len(files))
	}
	rec := &imagesEmbedRec{Type: "app.bsky.embed.images"}
	for i, f := range files {
		if f == nil || f.ID == "" {
			continue
		}
		data, err := ctx.Fetch(f)
		if err != nil {
			return nil, fmt.Errorf("取第 %d 张图失败: %w", i+1, err)
		}
		if len(data) > maxImgBytes {
			return nil, fmt.Errorf("第 %d 张图 %.1fMB，超过 Bluesky 的 2MB 上限（先压缩再发）",
				i+1, float64(len(data))/1e6)
		}
		blob, err := uploadBlob(ctx, data, mimeOf(f))
		if err != nil {
			return nil, fmt.Errorf("传第 %d 张图失败: %w", i+1, err)
		}
		alt := ""
		if i < len(alts) {
			alt = alts[i]
		}
		rec.Images = append(rec.Images, imageItem{Alt: alt, Image: blob})
	}
	if len(rec.Images) == 0 {
		return nil, fmt.Errorf("一张有效的图都没有")
	}
	return rec, nil
}

// externalEmbed：链接卡片。抓一次 OG 信息拼出来。
//
// **抓不到不是错误**（调用方会忽略 error 退回纯文本链接）：目标站可能挡爬虫、可能超时，
// 而那不该让一条本可以发出去的帖子失败。
func externalEmbed(ctx plugin.Ctx, link string) (*externalEmbedRec, error) {
	cred := credFrom(ctx)
	hc := clientFor(cred.Proxy)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	// 带一个正经 UA：不少站对空 UA 直接 403，那样卡片永远做不出来。
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SokelBot/1.0; +https://bsky.app)")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("目标站回 HTTP %d", resp.StatusCode)
	}
	head := make([]byte, 128<<10) // OG 标签在 <head> 里，读前 128KB 足够
	n, _ := resp.Body.Read(head)
	html := string(head[:n])

	rec := &externalEmbedRec{Type: "app.bsky.embed.external"}
	rec.External.URI = link
	rec.External.Title = firstNonEmpty(metaOf(html, "og:title"), titleOf(html), link)
	rec.External.Description = metaOf(html, "og:description")
	if img := metaOf(html, "og:image"); img != "" {
		if data, mime, err := fetchImage(ctx, hc, absURL(link, img)); err == nil && len(data) <= maxImgBytes {
			if blob, err := uploadBlob(ctx, data, mime); err == nil {
				rec.External.Thumb = blob
			}
		}
	}
	return rec, nil
}

func fetchImage(ctx plugin.Ctx, hc *http.Client, uri string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	buf := make([]byte, maxImgBytes+1)
	n, _ := readFull(resp.Body, buf)
	mime := resp.Header.Get("Content-Type")
	if i := strings.Index(mime, ";"); i > 0 {
		mime = mime[:i]
	}
	if mime == "" {
		mime = "image/jpeg"
	}
	return buf[:n], strings.TrimSpace(mime), nil
}

func readFull(r interface{ Read([]byte) (int, error) }, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := r.Read(buf[total:])
		total += n
		if err != nil || n == 0 {
			return total, err
		}
	}
	return total, nil
}

// —— OG 解析（正则够用：只取 head 里那几个 meta，不值得引一个 HTML 解析器）——

var metaRe = regexp.MustCompile(`(?is)<meta[^>]+>`)

func metaOf(html, property string) string {
	for _, tag := range metaRe.FindAllString(html, -1) {
		low := strings.ToLower(tag)
		if !strings.Contains(low, `"`+property+`"`) && !strings.Contains(low, `'`+property+`'`) &&
			!strings.Contains(low, "="+property+" ") {
			continue
		}
		if v := attrOf(tag, "content"); v != "" {
			return unescape(v)
		}
	}
	return ""
}

var titleRe = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

func titleOf(html string) string {
	if m := titleRe.FindStringSubmatch(html); len(m) == 2 {
		return unescape(strings.TrimSpace(m[1]))
	}
	return ""
}

func attrOf(tag, name string) string {
	re := regexp.MustCompile(`(?is)` + name + `\s*=\s*["']([^"']*)["']`)
	if m := re.FindStringSubmatch(tag); len(m) == 2 {
		return m[1]
	}
	return ""
}

func unescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'", "&nbsp;", " ")
	return strings.TrimSpace(r.Replace(s))
}

// absURL：og:image 常常是相对路径。
func absURL(base, ref string) string {
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	i := strings.Index(base, "://")
	if i < 0 {
		return ref
	}
	host := base[:i+3]
	if j := strings.Index(base[i+3:], "/"); j > 0 {
		host = base[:i+3+j]
	}
	if strings.HasPrefix(ref, "/") {
		return host + ref
	}
	return host + "/" + ref
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func mimeOf(f *plugin.File) string {
	if f != nil && strings.TrimSpace(f.Mime) != "" {
		return f.Mime
	}
	return "image/jpeg"
}
