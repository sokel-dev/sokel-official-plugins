package main

// GBK/GB2312 解码。国内不少 RSS 源还是这个编码，不解的话整份内容是乱码。
//
// 用 x/text 的现成解码器而不是自己写码表：GBK 有两万多个码位，手写表既大又必然出错，
// 而这个依赖本仓库别处已经在用（golang.org/x/crypto 同源），不算引入新的一片。

import (
	"bytes"
	"io"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// charsetReader：交给 xml.Decoder 用。认得的编码就转，认不得的原样放行——
// **不认识就报错是错的**：多数源其实是 UTF-8 却写了个奇怪的 charset 名。
func charsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "gbk", "gb2312", "gb-2312", "gb18030", "x-gbk":
		raw, err := io.ReadAll(input)
		if err != nil {
			return nil, err
		}
		out, _, err := transform.Bytes(simplifiedchinese.GB18030.NewDecoder(), raw)
		if err != nil {
			// 解不动就把原文交回去：个别字乱码好过整份内容作废。
			return bytes.NewReader(raw), nil
		}
		return bytes.NewReader(out), nil
	}
	return input, nil
}
