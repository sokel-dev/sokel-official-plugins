package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"strings"
	"testing"
)

// Public interoperability vector, not live credentials:
// https://developer.work.weixin.qq.com/document/path/90968
const callbackVectorKey = "jWmYm7qr5nMoAUwZRjGtBxmz3KA1tkAj3ykkR6q2B2C"
const callbackVectorCorp = "wx5823bf96d3bd56c7"
const callbackVectorCipher = "RypEvHKD8QQKFhvQ6QleEB4J58tiPdvo+rtK1I9qca6aM/wvqnLSV5zEPeusUiX5L5X/0lWfrf0QADHHhGd3QczcdCUpj911L3vg3W/sYYvuJTs3TUUkSUXxaccAS0qhxchrRYt66wiSpGLYL42aM6A8dTT+6k4aSknmPj48kzJs8qLjvd4Xgpue06DOdnLxAUHzM6+kDZ+HMZfJYuR+LtwGc2hgf5gsijff0ekUNXZiqATP7PF5mZxZ3Izoun1s4zG4LUMnvw2r+KqCKIw+3IQH03v+BCA9nMELNqbSf6tiWSrXJB3LAVGUcallcrw8V2t9EL4EhzJWrQUax5wLVMNS0+rUPA3k22Ncx4XXZS9o0MBH27Bo6BpNelZpS+/uh9KsNlY6bHCmJU9p8g7m3fVKn28H3KDYA5Pl/T8Z1ptDAVe0lXdQ2YoyyH2uyPIGHBZZIs2pDBS8R07+qN+E7Q=="
const callbackVectorSignature = "477715d11cdb4164915debcba66cb864d751f3e6"
const callbackVectorTimestamp = "1409659813"
const callbackVectorNonce = "1372623149"
const callbackVectorPlain = "<xml><ToUserName><![CDATA[wx5823bf96d3bd56c7]]></ToUserName>\n<FromUserName><![CDATA[mycreate]]></FromUserName>\n<CreateTime>1409659813</CreateTime>\n<MsgType><![CDATA[text]]></MsgType>\n<Content><![CDATA[hello]]></Content>\n<MsgId>4561255354251345929</MsgId>\n<AgentID>218</AgentID>\n</xml>"

func vectorCrypt(t *testing.T) *CallbackCrypt {
	t.Helper()
	c, err := NewCallbackCrypt("QDG6eK", callbackVectorKey, callbackVectorCorp)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestCallbackOfficialVector(t *testing.T) {
	c := vectorCrypt(t)
	got, err := c.Decrypt(callbackVectorSignature, callbackVectorTimestamp, callbackVectorNonce, callbackVectorCipher)
	if err != nil || string(got) != callbackVectorPlain {
		t.Fatalf("official decryption: %q, %v", got, err)
	}
	// The documented ciphertext has random prefix 0960688932c47ef1;
	// it was independently decoded with OpenSSL, not this implementation.
	reply, err := c.encryptReply([]byte(callbackVectorPlain), callbackVectorTimestamp, callbackVectorNonce, strings.NewReader("0960688932c47ef1"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Encrypt      string
		MsgSignature string
		TimeStamp    string
		Nonce        string
	}
	if err := xml.Unmarshal(reply, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Encrypt != callbackVectorCipher || envelope.MsgSignature != callbackVectorSignature || envelope.TimeStamp != callbackVectorTimestamp || envelope.Nonce != callbackVectorNonce {
		t.Fatalf("reply differs from official vector: %+v", envelope)
	}
}

func TestCallbackVerifyURL(t *testing.T) {
	c := vectorCrypt(t)
	// Official SDK's Sample URL verification vector (90307).
	echo := "P9nAzCzyDtyTWESHep1vC5X9xho/qYX3Zpb4yKa9SKld1DsH3Iyt3tP3zNdtp+4RPcs8TgAE7OaBO+FZXvnaqQ=="
	got, err := c.VerifyURL("5c45ff5e21c57e6ad56bac8758b79b1d9ac89fd3", "1409659589", "263014780", echo)
	if err != nil || string(got) != "1616140317555161061" {
		t.Fatalf("verification echo: %q, %v", got, err)
	}
}

func TestCallbackRejectsTampering(t *testing.T) {
	c := vectorCrypt(t)
	for name, values := range map[string][4]string{
		"signature":        {strings.Repeat("0", 40), callbackVectorTimestamp, callbackVectorNonce, callbackVectorCipher},
		"short signature":  {"abc", callbackVectorTimestamp, callbackVectorNonce, callbackVectorCipher},
		"signature nonhex": {strings.Repeat("z", 40), callbackVectorTimestamp, callbackVectorNonce, callbackVectorCipher},
		"timestamp":        {callbackVectorSignature, "1409659814", callbackVectorNonce, callbackVectorCipher},
		"nonce":            {callbackVectorSignature, callbackVectorTimestamp, "different", callbackVectorCipher},
		"cipher":           {callbackVectorSignature, callbackVectorTimestamp, callbackVectorNonce, "A" + callbackVectorCipher[1:]},
		"oversize":         {callbackVectorSignature, callbackVectorTimestamp, callbackVectorNonce, strings.Repeat("A", 2<<20)},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := c.Decrypt(values[0], values[1], values[2], values[3]); err == nil {
				t.Fatal("accepted tampered request")
			}
		})
	}
	other, err := NewCallbackCrypt("QDG6eK", callbackVectorKey, "another-corp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Decrypt(callbackVectorSignature, callbackVectorTimestamp, callbackVectorNonce, callbackVectorCipher); err == nil {
		t.Fatal("accepted wrong receiver")
	}
}

// Independently constructs malformed wire payloads after authentication, so the
// length/padding checks are exercised rather than only the signature check.
func TestCallbackRejectsMalformedCiphertext(t *testing.T) {
	c := vectorCrypt(t)
	key, _ := base64.StdEncoding.DecodeString(callbackVectorKey + "=")
	block, _ := aes.NewCipher(key)
	seal := func(plain []byte) string {
		out := make([]byte, len(plain))
		cipher.NewCBCEncrypter(block, key[:16]).CryptBlocks(out, plain)
		return base64.StdEncoding.EncodeToString(out)
	}
	valid := append(make([]byte, 20), []byte("hello"+callbackVectorCorp)...)
	binary.BigEndian.PutUint32(valid[16:20], 5)
	pad := 32 - len(valid)%32
	valid = append(valid, bytes.Repeat([]byte{byte(pad)}, pad)...)
	badPad := bytes.Clone(valid)
	badPad[len(badPad)-2] ^= 1
	zeroPad := bytes.Clone(valid)
	zeroPad[len(zeroPad)-1] = 0
	largePad := bytes.Clone(valid)
	largePad[len(largePad)-1] = 33
	badLength := bytes.Clone(valid)
	binary.BigEndian.PutUint32(badLength[16:20], ^uint32(0))
	tooShort := bytes.Repeat([]byte{32}, 32)
	for name, encrypted := range map[string]string{
		"base64 invalid":          "!malformed!",
		"base64 newline":          callbackVectorCipher[:20] + "\n" + callbackVectorCipher[20:],
		"base64 unpadded":         strings.TrimRight(callbackVectorCipher, "="),
		"block misalignment":      base64.StdEncoding.EncodeToString(make([]byte, 63)),
		"padding inconsistent":    seal(badPad),
		"padding zero":            seal(zeroPad),
		"padding excessive":       seal(largePad),
		"message length overflow": seal(badLength),
		"plain too short":         seal(tooShort),
	} {
		t.Run(name, func(t *testing.T) {
			sig := callbackSignature("QDG6eK", callbackVectorTimestamp, callbackVectorNonce, encrypted)
			if _, err := c.Decrypt(sig, callbackVectorTimestamp, callbackVectorNonce, encrypted); err == nil {
				t.Fatal("accepted malformed authenticated ciphertext")
			}
		})
	}
}

func TestCallbackConfigAndReplyBounds(t *testing.T) {
	for _, config := range [][3]string{
		{"", callbackVectorKey, callbackVectorCorp},
		{"token", "wrong", callbackVectorCorp},
		{"token", strings.Repeat("!", 43), callbackVectorCorp},
		{"token", callbackVectorKey, ""},
		{"token", callbackVectorKey, strings.Repeat("c", 257)},
	} {
		if _, err := NewCallbackCrypt(config[0], config[1], config[2]); err == nil {
			t.Fatal("accepted invalid configuration")
		}
	}
	c := vectorCrypt(t)
	if _, err := c.EncryptReply(make([]byte, 1<<20+1), callbackVectorTimestamp, callbackVectorNonce); err == nil {
		t.Fatal("accepted oversized reply")
	}
	if _, err := c.EncryptReply([]byte("test"), "not-a-timestamp", "nonce"); err == nil {
		t.Fatal("accepted malformed timestamp")
	}
	if _, err := c.encryptReply([]byte("test"), callbackVectorTimestamp, callbackVectorNonce, strings.NewReader("short")); err == nil {
		t.Fatal("accepted failed random source")
	}
	// An XML metacharacter in a valid nonce must not create extra elements.
	reply, err := c.EncryptReply([]byte("确认 <&> ]]>"), callbackVectorTimestamp, "<Nonce>&value")
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct{ Encrypt, MsgSignature, TimeStamp, Nonce string }
	if err := xml.Unmarshal(reply, &envelope); err != nil {
		t.Fatal(err)
	}
	got, err := c.Decrypt(envelope.MsgSignature, envelope.TimeStamp, envelope.Nonce, envelope.Encrypt)
	if err != nil || string(got) != "确认 <&> ]]>" || envelope.Nonce != "<Nonce>&value" {
		t.Fatalf("escaped XML reply round-trip: %q, %v", got, err)
	}
}
