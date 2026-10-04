package main

// Embeds: images and link cards.
//
// Both need the bytes uploaded as a blob first (com.atproto.repo.uploadBlob), then the
// returned blob ref gets put into the record. **An unreferenced blob gets garbage
// collected**, so the post must follow the upload immediately — it can't wait hours
// behind a human-approval node in between. This is also why images aren't exposed as a
// separate "upload" operation.

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const (
	maxImages   = 4
	maxImgBytes = 2_000_000 // Bluesky's per-image limit; exceeding it returns BlobTooLarge
)

// blobRef: uploadBlob's return value, put back into the record as-is.
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
	// uploadBlob accepts raw bytes + the real Content-Type (not JSON).
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

// externalEmbed: a link card, built by fetching OG info once.
//
// **A failed fetch is not an error** (the caller ignores the error and falls back to a
// plain text link): the target site might block crawlers or time out, and that shouldn't
// fail a post that could otherwise go out fine.
func externalEmbed(ctx plugin.Ctx, link string) (*externalEmbedRec, error) {
	cred := credFrom(ctx)
	hc := clientFor(cred.Proxy)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil, err
	}
	// Send a real UA: plenty of sites 403 an empty UA outright, which would make the card impossible to build.
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; SokelBot/1.0; +https://bsky.app)")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("目标站回 HTTP %d", resp.StatusCode)
	}
	head := make([]byte, 128<<10) // OG tags live in <head>, the first 128KB is plenty
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

// —— OG parsing (regex is good enough: it only reads a few meta tags from head, not worth pulling in an HTML parser) ——

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

// absURL: og:image is often a relative path.
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
