package main

// Media upload: INIT -> APPEND x N -> FINALIZE -> (video) wait for transcoding.
//
// Why not a "simple upload": X's one-shot upload only accepts small images, while video/GIF must be
// chunked. Supporting both paths would make "swap the file and it breaks" the norm, so everything
// goes through chunked upload — an image just happens to be a single chunk.
//
// Transcoding must be waited out: right after FINALIZE, a video is pending/in_progress, and using
// its media_id to post at that point gets rejected ("media not found", which looks like a wrong
// id). So this waits until the state is succeeded before returning.

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

const (
	// chunkSize: X caps a single chunk at 5MB; 4MB leaves headroom (the multipart headers count
	// toward the limit too).
	chunkSize = 4 << 20
	// mediaWaitCap: the total time budget for waiting on transcoding. Past this, report the
	// media_id along with its current state — a large video genuinely can take a few minutes, and
	// telling someone which step it's stuck at is more useful than a bare "timeout".
	mediaWaitCap = 8 * time.Minute
)

type mediaData struct {
	Data struct {
		ID             string `json:"id"`
		MediaKey       string `json:"media_key"`
		Size           int    `json:"size"`
		ProcessingInfo struct {
			State         string `json:"state"`
			ProgressPct   int    `json:"progress_percent"`
			CheckAfterSec int    `json:"check_after_secs"`
			Error         struct {
				Name    string `json:"name"`
				Message string `json:"message"`
			} `json:"error"`
		} `json:"processing_info"`
	} `json:"data"`
}

func opMediaUpload(ctx plugin.Ctx, in *XMediaUploadIn) (*XMediaUploadOut, error) {
	if in.File == nil || in.File.ID == "" {
		return nil, fmt.Errorf("没有文件")
	}
	blob, err := ctx.Fetch(in.File)
	if err != nil {
		return nil, fmt.Errorf("取文件失败: %w", err)
	}
	if len(blob) == 0 {
		return nil, fmt.Errorf("文件是空的")
	}
	mime := strings.TrimSpace(in.File.Mime)
	if mime == "" {
		mime = sniffMime(in.File.Name)
	}
	category, err := mediaCategory(mime, in.Purpose)
	if err != nil {
		return nil, err
	}

	// INIT
	var init mediaData
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/media/upload/initialize", body: map[string]any{
		"media_type": mime, "total_bytes": len(blob), "media_category": category,
	}}, &init); err != nil {
		return nil, fmt.Errorf("初始化上传失败: %w", err)
	}
	id := init.Data.ID
	if id == "" {
		return nil, fmt.Errorf("X 没返回 media id")
	}

	// APPEND: chunked. X's chunk index starts at 0 and must be contiguous.
	for i, off := 0, 0; off < len(blob); i, off = i+1, off+chunkSize {
		end := min(off+chunkSize, len(blob))
		body, ctype, err := chunkForm(blob[off:end], i, in.File.Name)
		if err != nil {
			return nil, err
		}
		if err := callAPI(ctx, reqOpts{method: http.MethodPost,
			path: "/media/upload/" + id + "/append", raw: body, rawType: ctype}, nil); err != nil {
			return nil, fmt.Errorf("上传第 %d 片失败: %w", i+1, err)
		}
	}

	// FINALIZE
	var fin mediaData
	if err := callAPI(ctx, reqOpts{method: http.MethodPost, path: "/media/upload/" + id + "/finalize"}, &fin); err != nil {
		return nil, fmt.Errorf("结束上传失败: %w", err)
	}
	state, err := waitProcessed(ctx, id, fin)
	if err != nil {
		return nil, err
	}

	// alt text is a separate call, and failing it **doesn't count as an overall failure**: the
	// media already uploaded fine, and invalidating that over a description would mean re-uploading
	// a tens-of-megabytes video all over again on retry.
	if alt := strings.TrimSpace(in.AltText); alt != "" {
		if err := setAltText(ctx, id, alt); err != nil {
			return &XMediaUploadOut{MediaID: id, MediaKey: fin.Data.MediaKey, Size: len(blob),
				State: state + "（替代文本没设上：" + err.Error() + "）"}, nil
		}
	}
	return &XMediaUploadOut{MediaID: id, MediaKey: fin.Data.MediaKey, Size: len(blob), State: state}, nil
}

// waitProcessed waits until transcoding finishes. An image usually has no processing_info right
// after FINALIZE and returns immediately.
func waitProcessed(ctx plugin.Ctx, id string, fin mediaData) (string, error) {
	state := fin.Data.ProcessingInfo.State
	if state == "" || state == "succeeded" {
		return "succeeded", nil
	}
	wait := fin.Data.ProcessingInfo.CheckAfterSec
	deadline := time.Now().Add(mediaWaitCap)
	for time.Now().Before(deadline) {
		if wait <= 0 {
			wait = 2
		}
		t := time.NewTimer(time.Duration(wait) * time.Second)
		select {
		case <-t.C:
		case <-ctx.Done():
			t.Stop()
			return state, ctx.Err()
		}
		var st mediaData
		if err := callAPI(ctx, reqOpts{method: http.MethodGet, path: "/media/upload",
			query: url.Values{"media_id": {id}, "command": {"STATUS"}}}, &st); err != nil {
			return state, fmt.Errorf("查转码状态失败: %w", err)
		}
		info := st.Data.ProcessingInfo
		switch info.State {
		case "succeeded", "":
			return "succeeded", nil
		case "failed":
			return "failed", fmt.Errorf("X 转码失败：%s %s（视频要 MP4/H.264/AAC，且时长 ≤140 秒）",
				info.Error.Name, info.Error.Message)
		}
		state, wait = info.State, info.CheckAfterSec
	}
	return state, fmt.Errorf("等转码超时（media_id=%s，当前状态 %s）：可以稍后用这个 id 直接发推试试", id, state)
}

func setAltText(ctx plugin.Ctx, id, alt string) error {
	if len([]rune(alt)) > 1000 {
		alt = string([]rune(alt)[:1000])
	}
	return callAPI(ctx, reqOpts{method: http.MethodPost, path: "/media/metadata", body: map[string]any{
		"id":       id,
		"metadata": map[string]any{"alt_text": map[string]any{"text": alt}},
	}}, nil)
}

// chunkForm builds one chunk's multipart request body.
func chunkForm(chunk []byte, index int, name string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	if err := w.WriteField("segment_index", fmt.Sprint(index)); err != nil {
		return nil, "", err
	}
	if name == "" {
		name = "media"
	}
	part, err := w.CreateFormFile("media", name)
	if err != nil {
		return nil, "", err
	}
	if _, err := part.Write(chunk); err != nil {
		return nil, "", err
	}
	if err := w.Close(); err != nil {
		return nil, "", err
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}

// mediaCategory maps mime/purpose to X's category (get the purpose wrong, and the media_id only
// gets rejected at the publish step).
func mediaCategory(mime, purpose string) (string, error) {
	dm := purpose == "dm"
	switch {
	case mime == "image/gif":
		if dm {
			return "dm_gif", nil
		}
		return "tweet_gif", nil
	case strings.HasPrefix(mime, "image/"):
		if dm {
			return "dm_image", nil
		}
		return "tweet_image", nil
	case strings.HasPrefix(mime, "video/"):
		if dm {
			return "dm_video", nil
		}
		return "tweet_video", nil
	}
	return "", fmt.Errorf("X 不收这种文件（%s）：只支持图片、GIF 与视频", mime)
}

func sniffMime(name string) string {
	switch {
	case strings.HasSuffix(strings.ToLower(name), ".png"):
		return "image/png"
	case strings.HasSuffix(strings.ToLower(name), ".gif"):
		return "image/gif"
	case strings.HasSuffix(strings.ToLower(name), ".mp4"):
		return "video/mp4"
	case strings.HasSuffix(strings.ToLower(name), ".webp"):
		return "image/webp"
	}
	return "image/jpeg"
}
