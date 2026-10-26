package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime"
	"net/http"
	"strings"
	"testing"
)

// The success response and multipart part names follow official document 90253.
// HTTP assertions inspect real multipart bytes, without uploading tenant assets.
func TestUploadMediaMultipartWireAndTokenRetry(t *testing.T) {
	tokens, uploads := 0, 0
	var uploaded [][]byte
	filename := `测试 "report".txt`
	contents := []byte("hello\x00media\n")
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			tokens++
			fmt.Fprintf(w, `{"errcode":0,"access_token":"TOKEN_%d","expires_in":7200}`, tokens)
		case "/cgi-bin/media/upload":
			uploads++
			if r.Method != http.MethodPost || r.URL.Query().Get("type") != "file" || r.URL.Query().Get("access_token") != fmt.Sprintf("TOKEN_%d", uploads) {
				t.Errorf("incorrect upload route/method/query: %s %s", r.Method, r.URL.Path)
			}
			reader, err := r.MultipartReader()
			if err != nil {
				t.Fatal(err)
			}
			part, err := reader.NextPart()
			if err != nil {
				t.Fatal(err)
			}
			_, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
			if err != nil || part.FormName() != "media" || part.FileName() != filename || params["filelength"] != fmt.Sprint(len(contents)) || part.Header.Get("Content-Type") != "application/octet-stream" {
				t.Errorf("wrong multipart headers: %v %v", part.Header, err)
			}
			data, _ := io.ReadAll(part)
			uploaded = append(uploaded, data)
			if _, err := reader.NextPart(); err != io.EOF {
				t.Errorf("unexpected extra part: %v", err)
			}
			if uploads == 1 {
				io.WriteString(w, `{"errcode":42001,"errmsg":"access_token expired TOKEN_1 TEST-SECRET"}`)
				return
			}
			io.WriteString(w, `{"errcode":0,"errmsg":"","type":"file","media_id":"MEDIA_ID","created_at":"1380000000"}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(404)
		}
	})
	media, err := c.UploadMedia(context.Background(), clientTestCred(), "file", filename, contents)
	if err != nil {
		t.Fatal(err)
	}
	if media != (WeComMedia{MediaID: "MEDIA_ID", Type: "file", CreatedAt: 1380000000}) {
		t.Fatalf("wrong media result: %+v", media)
	}
	if tokens != 2 || uploads != 2 || !bytes.Equal(uploaded[0], contents) || !bytes.Equal(uploaded[1], contents) {
		t.Fatalf("token refresh lost/repeated upload incorrectly: tokens=%d uploads=%d", tokens, uploads)
	}
}

func TestUploadMediaResponseFailuresDoNotRetry(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{"http failure", 502, "TEST-SECRET"},
		{"business failure", 200, `{"errcode":40005,"errmsg":"bad TEST-SECRET"}`},
		{"malformed response", 200, `{"errcode":0`},
		{"missing envelope", 200, `{"type":"file","media_id":"MEDIA_ID","created_at":"1380000000"}`},
		{"missing media id", 200, `{"errcode":0,"type":"file","created_at":"1380000000"}`},
		{"mismatched type", 200, `{"errcode":0,"type":"image","media_id":"MEDIA_ID","created_at":"1380000000"}`},
		{"missing timestamp", 200, `{"errcode":0,"type":"file","media_id":"MEDIA_ID"}`},
		{"invalid timestamp", 200, `{"errcode":0,"type":"file","media_id":"MEDIA_ID","created_at":"TEST-SECRET"}`},
		{"overflow timestamp", 200, `{"errcode":0,"type":"file","media_id":"MEDIA_ID","created_at":9223372036854775808}`},
		{"fractional timestamp", 200, `{"errcode":0,"type":"file","media_id":"MEDIA_ID","created_at":123.5}`},
		{"zero timestamp", 200, `{"errcode":0,"type":"file","media_id":"MEDIA_ID","created_at":0}`},
		{"too large", 200, strings.Repeat("x", 1024*1024+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/cgi-bin/gettoken" {
					io.WriteString(w, documentedTokenResponse)
					return
				}
				calls++
				w.WriteHeader(tc.status)
				io.WriteString(w, tc.body)
			})
			_, err := c.UploadMedia(context.Background(), clientTestCred(), "file", "test.txt", []byte("123456"))
			if err == nil || strings.Contains(err.Error(), "TEST-SECRET") || calls != 1 {
				t.Fatalf("failure leaked data, was accepted or retried: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestUploadMediaNumericTimestamp(t *testing.T) {
	c := newTestWeComClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			io.WriteString(w, documentedTokenResponse)
		} else {
			io.WriteString(w, `{"errcode":0,"type":"file","media_id":"MEDIA_ID","created_at":1380000000}`)
		}
	})
	media, err := c.UploadMedia(context.Background(), clientTestCred(), "file", "test.txt", []byte("123456"))
	if err != nil || media.CreatedAt != 1380000000 {
		t.Fatalf("numeric timestamp failed: %+v %v", media, err)
	}
}

type uploadRoundTripper func(*http.Request) (*http.Response, error)

func (fn uploadRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestUploadMediaTransportFailureDoesNotLeakOrRetry(t *testing.T) {
	c := NewWeComClient()
	calls := 0
	c.httpClient.Transport = uploadRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path == "/cgi-bin/gettoken" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(documentedTokenResponse)), Header: make(http.Header)}, nil
		}
		calls++
		return nil, errors.New("network failed " + r.URL.String() + " TEST-SECRET")
	})
	_, err := c.UploadMedia(context.Background(), clientTestCred(), "file", "test.txt", []byte("123456"))
	if err == nil || strings.Contains(err.Error(), "TEST-SECRET") || strings.Contains(err.Error(), "ACCESS_TOKEN") || calls != 1 || !strings.Contains(err.Error(), "上传结果未知") {
		t.Fatalf("unsafe transport error/retry: calls=%d err=%v", calls, err)
	}
}

func TestUploadMediaValidation(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var pngBytes, jpegBytes bytes.Buffer
	if err := png.Encode(&pngBytes, img); err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(&jpegBytes, img, nil); err != nil {
		t.Fatal(err)
	}
	// ISO BMFF file type header, sufficient for local container-signature checking.
	mp4 := []byte{0, 0, 0, 20, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm', 0, 0, 0, 0, 'i', 's', 'o', 'm'}
	amr := append([]byte("#!AMR\n"), append([]byte{0x04}, make([]byte, 12)...)...)
	cases := []struct {
		name, kind, filename, contentType string
		data                              []byte
		valid                             bool
	}{
		{"PNG", "image", "图片.PNG", "image/png", pngBytes.Bytes(), true},
		{"JPEG", "image", "photo.jpeg", "image/jpeg", jpegBytes.Bytes(), true},
		{"AMR", "voice", "voice.amr", "audio/amr", amr, true},
		{"MP4", "video", "clip.mp4", "video/mp4", mp4, true},
		{"file", "file", "数据.csv", "application/octet-stream", []byte("123456"), true},
		{"5 bytes", "file", "test.txt", "", []byte("12345"), false},
		{"unknown kind", "audio", "test.amr", "", amr, false},
		{"path", "file", "../../test.txt", "", []byte("123456"), false},
		{"windows path", "file", `C:\test.txt`, "", []byte("123456"), false},
		{"header injection", "file", "test\r\nX-Extra: y", "", []byte("123456"), false},
		{"invalid UTF8 filename", "file", string([]byte{'a', 0xff}), "", []byte("123456"), false},
		{"bad png", "image", "test.png", "", []byte("not a png"), false},
		{"mismatched extension", "image", "test.jpg", "", pngBytes.Bytes(), false},
		{"wrong video extension", "video", "test.mov", "", mp4, false},
		{"bad mp4", "video", "test.mp4", "", []byte("not a video"), false},
		{"bad AMR", "voice", "test.amr", "", []byte("not an AMR"), false},
		{"image too large", "image", "test.png", "", make([]byte, 10*1024*1024+1), false},
		{"voice too large", "voice", "test.amr", "", make([]byte, 2*1024*1024+1), false},
		{"video too large", "video", "test.mp4", "", make([]byte, 10*1024*1024+1), false},
		{"file too large", "file", "test.bin", "", make([]byte, 20*1024*1024+1), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			contentType, err := validateWeComMedia(tc.kind, tc.filename, tc.data)
			if (err == nil) != tc.valid || contentType != tc.contentType {
				t.Fatalf("contentType=%q err=%v, want contentType=%q valid=%v", contentType, err, tc.contentType, tc.valid)
			}
		})
	}
}

func TestAMRDurationLimits(t *testing.T) {
	frame := append([]byte{0x04}, make([]byte, 12)...)
	for _, count := range []int{1, 3000, 3001} {
		data := append([]byte("#!AMR\n"), bytes.Repeat(frame, count)...)
		err := validateAMRDuration(data)
		if (err == nil) != (count <= 3000) {
			t.Errorf("frames=%d err=%v", count, err)
		}
	}
	for _, suffix := range [][]byte{nil, {0x04}, {0x4c}, {0x84}, {0x05}} {
		if err := validateAMRDuration(append([]byte("#!AMR\n"), suffix...)); err == nil {
			t.Errorf("accepted invalid AMR suffix %x", suffix)
		}
	}
	wideband := append([]byte("#!AMR-WB\n"), append([]byte{0x04}, make([]byte, 17)...)...)
	if err := validateAMRDuration(wideband); err != nil {
		t.Fatal(err)
	}
}
