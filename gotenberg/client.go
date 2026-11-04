package main

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxPDF = 64 << 20

func (c *client) healthCheck(ctx plugin.Ctx, in *HealthCheckIn) (*HealthCheckOut, error) {
	b, h, e := c.request(ctx, "GET", "/health", nil, http.Header{"Accept": {"application/json"}}, 65536)
	if e != nil {
		return &HealthCheckOut{OK: false, Message: e.Error()}, nil
	}
	var r struct {
		Status string `json:"status"`
	}
	if mediaType(h, "application/json") != nil || decodeJSON(b, &r) != nil || r.Status != "up" {
		return &HealthCheckOut{OK: false, Message: "Gotenberg did not report a healthy service."}, nil
	}
	return &HealthCheckOut{OK: true, Message: "Gotenberg reports a healthy service. Conversion access and document support must be checked separately."}, nil
}

type uploadPart struct {
	name string
	data []byte
}

func multipartBody(parts []uploadPart, fields map[string]string) ([]byte, string, error) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, p := range parts {
		h := make(textproto.MIMEHeader)
		h.Set("Content-Disposition", `form-data; name="files"; filename="`+quotedFilename(p.name)+`"`)
		h.Set("Content-Type", "application/octet-stream")
		part, e := w.CreatePart(h)
		if e != nil {
			return nil, "", errors.New("Could not build the multipart request.")
		}
		if _, e = part.Write(p.data); e != nil {
			return nil, "", errors.New("Could not write the multipart file.")
		}
	}
	for k, v := range fields {
		if e := w.WriteField(k, v); e != nil {
			return nil, "", errors.New("Could not write multipart options.")
		}
	}
	if e := w.Close(); e != nil {
		return nil, "", errors.New("Could not finish the multipart request.")
	}
	if buf.Len() > maxInput+(1<<20) {
		return nil, "", errors.New("Multipart request exceeds the 33 MiB limit.")
	}
	return buf.Bytes(), w.FormDataContentType(), nil
}
func outputName(value, fallback string) (string, error) {
	if value == "" {
		value = fallback
	}
	if !validFilename(value) || !strings.HasSuffix(strings.ToLower(value), ".pdf") {
		return "", errors.New("Output filename must be a plain filename ending in .pdf.")
	}
	return value, nil
}
func (c *client) convert(ctx plugin.Ctx, route, name string, parts []uploadPart, fields map[string]string) (*plugin.File, error) {
	data, ct, e := multipartBody(parts, fields)
	if e != nil {
		return nil, e
	}
	b, h, e := c.request(ctx, "POST", route, data, http.Header{"Content-Type": {ct}, "Accept": {"application/pdf"}}, maxPDF)
	if e != nil {
		return nil, e
	}
	if mediaType(h, "application/pdf") != nil || !bytes.HasPrefix(b, []byte("%PDF-")) {
		return nil, errors.New("Gotenberg did not return a PDF document.")
	}
	tail := b
	if len(tail) > 1024 {
		tail = tail[len(tail)-1024:]
	}
	if !bytes.Contains(tail, []byte("%%EOF")) {
		return nil, errors.New("Gotenberg returned an incomplete PDF document.")
	}
	f, e := ctx.Upload(name, "application/pdf", b)
	if e != nil || f == nil {
		return nil, errors.New("Could not store the PDF in the platform.")
	}
	return f, nil
}
func (c *client) htmlToPDF(ctx plugin.Ctx, in *HTMLToPdfIn) (*HTMLToPdfOut, error) {
	if in.HTML == "" || !utf8.ValidString(in.HTML) || strings.ContainsRune(in.HTML, 0) || len(in.HTML) > 2<<20 {
		return nil, errors.New("Provide a UTF-8 HTML document of at most 2 MiB without NUL characters.")
	}
	name, e := outputName(in.OutputName, "document.pdf")
	if e != nil {
		return nil, e
	}
	if len(in.Assets) > 20 {
		return nil, errors.New("Provide at most 20 asset files.")
	}
	parts := []uploadPart{{"index.html", []byte(in.HTML)}}
	total := len(in.HTML)
	names := map[string]bool{"index.html": true, "header.html": true, "footer.html": true}
	for _, f := range in.Assets {
		if f == nil || names[strings.ToLower(f.Name)] {
			return nil, errors.New("Asset filenames must be unique and cannot be index.html, header.html or footer.html.")
		}
		b, e := readFile(ctx, f)
		if e != nil {
			return nil, e
		}
		total += len(b)
		if total > maxInput {
			return nil, errors.New("HTML and assets together exceed 32 MiB.")
		}
		names[strings.ToLower(f.Name)] = true
		parts = append(parts, uploadPart{f.Name, b})
	}
	fields := map[string]string{}
	for key, v := range map[string]*bool{"landscape": in.Options.Landscape, "printBackground": in.Options.PrintBackground, "preferCssPageSize": in.Options.PreferCSSPageSize} {
		if v != nil {
			fields[key] = strconv.FormatBool(*v)
		}
	}
	f, e := c.convert(ctx, "/forms/chromium/convert/html", name, parts, fields)
	if e != nil {
		return nil, e
	}
	return &HTMLToPdfOut{File: f}, nil
}
func (c *client) officeToPDF(ctx plugin.Ctx, in *OfficeToPdfIn) (*OfficeToPdfOut, error) {
	name, e := outputName(in.OutputName, "document.pdf")
	if e != nil {
		return nil, e
	}
	if in.File == nil {
		return nil, errors.New("Provide an office document.")
	}
	ext := strings.ToLower(path.Ext(in.File.Name))
	switch ext {
	case ".doc", ".docx", ".docm", ".odt", ".rtf", ".txt", ".xls", ".xlsx", ".xlsm", ".ods", ".csv", ".ppt", ".pptx", ".pptm", ".odp":
	default:
		return nil, errors.New("Unsupported office document extension. Use a supported Word, spreadsheet, presentation or text document.")
	}
	b, e := readFile(ctx, in.File)
	if e != nil {
		return nil, e
	}
	if len(b) == 0 {
		return nil, errors.New("Office document is empty.")
	}
	f, e := c.convert(ctx, "/forms/libreoffice/convert", name, []uploadPart{{in.File.Name, b}}, nil)
	if e != nil {
		return nil, e
	}
	return &OfficeToPdfOut{File: f}, nil
}
func (c *client) mergePDFs(ctx plugin.Ctx, in *MergePdfsIn) (*MergePdfsOut, error) {
	if len(in.Files) < 2 || len(in.Files) > 20 {
		return nil, errors.New("Provide 2 to 20 PDF files in the required order.")
	}
	name, e := outputName(in.OutputName, "merged.pdf")
	if e != nil {
		return nil, e
	}
	parts := make([]uploadPart, 0, len(in.Files))
	total := 0
	for i, f := range in.Files {
		b, e := readFile(ctx, f)
		if e != nil {
			return nil, e
		}
		if !bytes.HasPrefix(b, []byte("%PDF-")) {
			return nil, errors.New("Every merge input must contain PDF bytes.")
		}
		total += len(b)
		if total > maxInput {
			return nil, errors.New("Merge inputs together exceed 32 MiB.")
		}
		parts = append(parts, uploadPart{fmt.Sprintf("%04d.pdf", i+1), b})
	}
	f, e := c.convert(ctx, "/forms/pdfengines/merge", name, parts, nil)
	if e != nil {
		return nil, e
	}
	return &MergePdfsOut{File: f}, nil
}
