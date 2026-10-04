package main

// GBK/GB2312 decoding. Plenty of domestic RSS feeds still use this encoding; without decoding,
// the whole body comes out garbled.
//
// We use x/text's ready-made decoder instead of writing our own code table: GBK has over
// twenty thousand code points, so a hand-written table would be large and bound to have bugs,
// and this dependency is already used elsewhere in this repo (same family as golang.org/x/crypto),
// so it's not pulling in a new one.

import (
	"bytes"
	"io"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// charsetReader is handed to xml.Decoder. Known encodings get converted; unknown ones pass
// through unchanged — **erroring out on an unknown encoding would be wrong**: most feeds are
// actually UTF-8 but declare a weird charset name.
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "gbk", "gb2312", "gb-2312", "gb18030", "x-gbk":
		raw, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
		if err != nil {
			// If decoding fails, hand back the raw bytes: a few garbled characters beat
			// discarding the whole body.
			return bytes.NewReader(raw), nil
		}
		return bytes.NewReader(out), nil
	}
	return input, nil
}
