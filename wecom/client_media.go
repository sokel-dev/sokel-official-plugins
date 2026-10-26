package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type WeComMedia struct {
	MediaID   string `json:"media_id"`
	Type      string `json:"type"`
	CreatedAt int64  `json:"created_at"`
}

// UploadMedia follows document/path/90253. Temporary media IDs expire after
// three days. The destination is the fixed WeCom API host; callers supply bytes,
// never a URL to fetch. Only an explicit authentication rejection is retried.
// Network errors may follow a successful upload and must not be blindly retried.
func (c *WeComClient) UploadMedia(ctx context.Context, cred Cred, mediaType, filename string, data []byte) (WeComMedia, error) {
	var result WeComMedia
	cred, _, err := normalizedWeComCred(cred)
	if err != nil {
		return result, err
	}
	contentType, err := validateWeComMedia(mediaType, filename, data)
	if err != nil {
		return result, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	escapedName := strings.NewReplacer("\\", "\\\\", `"`, `\"`).Replace(filename)
	header.Set("Content-Disposition", `form-data; name="media"; filename="`+escapedName+`"; filelength=`+strconv.Itoa(len(data)))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return result, errors.New("无法构造企业微信素材请求")
	}
	if _, err := part.Write(data); err != nil {
		return result, errors.New("无法编码企业微信素材请求")
	}
	if err := writer.Close(); err != nil {
		return result, errors.New("无法完成企业微信素材请求")
	}
	for attempt := 0; attempt < 2; attempt++ {
		token, err := c.AccessToken(ctx, cred)
		if err != nil {
			return result, err
		}
		b, err := c.uploadMediaRequest(ctx, mediaType, token, writer.FormDataContentType(), body.Bytes())
		if isWeComTokenError(err) {
			c.invalidateToken(cred, token)
			if attempt == 0 {
				continue
			}
		}
		if err != nil {
			return result, err
		}
		var out struct {
			MediaID   string      `json:"media_id"`
			Type      string      `json:"type"`
			CreatedAt json.Number `json:"created_at"`
		}
		if err := json.Unmarshal(b, &out); err != nil {
			return result, errors.New("企业微信素材响应字段格式错误；上传结果未知")
		}
		createdAt, err := out.CreatedAt.Int64()
		if err != nil || createdAt <= 0 || strings.TrimSpace(out.MediaID) == "" || out.Type != mediaType {
			return result, errors.New("企业微信素材响应缺少有效 media_id、type 或 created_at；上传结果未知")
		}
		return WeComMedia{MediaID: out.MediaID, Type: out.Type, CreatedAt: createdAt}, nil
	}
	panic("unreachable")
}

func validateWeComMedia(mediaType, filename string, data []byte) (string, error) {
	if strings.TrimSpace(filename) == "" || !utf8.ValidString(filename) || len(filename) > 255 || strings.ContainsAny(filename, `/\`) || filename == "." || filename == ".." {
		return "", errors.New("素材文件名必须是有效的文件名，不能包含路径，且不能超过 255 字节")
	}
	for _, ch := range filename {
		if unicode.IsControl(ch) {
			return "", errors.New("素材文件名不能包含控制字符")
		}
	}
	maxBytes := 0
	switch mediaType {
	case "image", "video":
		maxBytes = 10 * 1024 * 1024
	case "voice":
		maxBytes = 2 * 1024 * 1024
	case "file":
		maxBytes = 20 * 1024 * 1024
	default:
		return "", errors.New("素材类型仅支持 image、voice、video、file")
	}
	if len(data) <= 5 || len(data) > maxBytes {
		return "", fmt.Errorf("素材必须大于 5 字节，%s 类型不能超过 %d MiB", mediaType, maxBytes/(1024*1024))
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch mediaType {
	case "image":
		_, format, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil || (format != "jpeg" && format != "png") {
			return "", errors.New("图片素材仅支持有效的 JPG 或 PNG 文件")
		}
		if format == "jpeg" && (ext == ".jpg" || ext == ".jpeg") {
			return "image/jpeg", nil
		}
		if format == "png" && ext == ".png" {
			return "image/png", nil
		}
		return "", errors.New("图片素材的扩展名必须与 JPG 或 PNG 内容一致")
	case "voice":
		if ext != ".amr" {
			return "", errors.New("语音素材仅支持 AMR 文件")
		}
		if err := validateAMRDuration(data); err != nil {
			return "", err
		}
		if bytes.HasPrefix(data, []byte("#!AMR-WB\n")) {
			return "audio/amr-wb", nil
		}
		return "audio/amr", nil
	case "video":
		if ext != ".mp4" || !hasMP4FileTypeBox(data) {
			return "", errors.New("视频素材仅支持 MP4 文件")
		}
		return "video/mp4", nil
	default:
		return "application/octet-stream", nil
	}
}

// RFC 4867 sections 3, 5: AMR storage frames represent 20 ms.
// Parse both narrowband and wideband storage
// headers instead of estimating duration from compressed bytes / bitrate.
func validateAMRDuration(data []byte) error {
	frameBytes := []int{13, 14, 16, 18, 20, 21, 27, 32, 6, 0, 0, 0, 0, 0, 0, 1}
	position := 0
	switch {
	case bytes.HasPrefix(data, []byte("#!AMR\n")):
		position = 6
	case bytes.HasPrefix(data, []byte("#!AMR-WB\n")):
		position = 9
		frameBytes = []int{18, 24, 33, 37, 41, 47, 51, 59, 61, 6, 0, 0, 0, 0, 1, 1}
	default:
		return errors.New("语音素材必须是有效的 AMR 文件")
	}
	frames := 0
	for position < len(data) {
		header := data[position]
		size := frameBytes[(header>>3)&0x0f]
		if header&0x83 != 0 || size == 0 || position+size > len(data) {
			return errors.New("AMR 文件包含无效或不完整的音频帧")
		}
		position += size
		frames++
		if frames > 3000 {
			return errors.New("语音素材播放长度不能超过 60 秒")
		}
	}
	if frames == 0 {
		return errors.New("AMR 文件至少需要一个音频帧")
	}
	return nil
}

// Inspect the ISO BMFF ftyp box without decoding video or trusting the suffix.
// Upstream validates codec support and complete media contents.
func hasMP4FileTypeBox(data []byte) bool {
	for position, boxes := 0, 0; position+8 <= len(data) && boxes < 32; boxes++ {
		size := uint64(binary.BigEndian.Uint32(data[position : position+4]))
		headerSize := uint64(8)
		if size == 1 {
			if position+16 > len(data) {
				return false
			}
			size = binary.BigEndian.Uint64(data[position+8 : position+16])
			headerSize = 16
		}
		if size < headerSize || size > uint64(len(data)-position) {
			return false
		}
		if string(data[position+4:position+8]) == "ftyp" {
			return size >= headerSize+8
		}
		position += int(size)
	}
	return false
}

func (c *WeComClient) uploadMediaRequest(ctx context.Context, mediaType, token, contentType string, body []byte) ([]byte, error) {
	query := url.Values{"access_token": {token}, "type": {mediaType}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/cgi-bin/media/upload?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, errors.New("无法构造企业微信素材上传请求")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", contentType)
	// Multipart uploads may be 20 MiB; keep transport and redirect protection,
	// but allow more time than the shared eight-second metadata API timeout.
	uploadClient := *c.httpClient
	uploadClient.Timeout = 45 * time.Second
	resp, err := uploadClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("企业微信素材上传已取消：%w；上传结果未知", ctx.Err())
		}
		var networkErr net.Error
		if errors.As(err, &networkErr) && networkErr.Timeout() {
			return nil, errors.New("企业微信素材上传超时；上传结果未知，请先核实后再重试")
		}
		return nil, errors.New("企业微信素材上传网络错误；上传结果未知，请检查网络后再核实")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &weComAPIError{status: resp.StatusCode}
	}
	const maxResponseBytes = 1024 * 1024
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return nil, errors.New("企业微信素材响应读取失败或超过大小限制；上传结果未知")
	}
	var envelope struct {
		ErrCode *int `json:"errcode"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil || envelope.ErrCode == nil {
		return nil, errors.New("企业微信素材响应不是包含 errcode 的有效 JSON；上传结果未知")
	}
	if *envelope.ErrCode != 0 {
		return nil, &weComAPIError{code: *envelope.ErrCode}
	}
	return b, nil
}
