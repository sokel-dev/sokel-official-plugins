package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha1" // Required by the authenticated WeCom callback protocol.
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"io"
	"sort"
	"strings"
)

const maxCallbackPlaintext = 1 << 20
const maxCallbackEncrypted = ((maxCallbackPlaintext+20+256+32)/32*32 + 2) / 3 * 4

// ErrCallbackCrypto intentionally does not disclose whether a request failed
// its signature, padding, length or receiver check to the public caller.
var ErrCallbackCrypto = errors.New("企业微信回调校验失败")

// CallbackCrypt implements the self-built application callback format from
// https://developer.work.weixin.qq.com/document/path/90968. It is immutable and
// safe to share between goroutines. The receive ID is always this app's CorpID.
type CallbackCrypt struct {
	token  string
	corpID string
	key    [32]byte
}

func NewCallbackCrypt(token, encodingAESKey, corpID string) (*CallbackCrypt, error) {
	if token == "" || len(token) > 256 || corpID == "" || len(corpID) > 256 || len(encodingAESKey) != 43 {
		return nil, ErrCallbackCrypto
	}
	for _, ch := range encodingAESKey {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '+' || ch == '/') {
			return nil, ErrCallbackCrypto
		}
	}
	// Unlike ciphertext, the published EncodingAESKey sample has nonzero unused
	// base64 bits. Standard decoding is necessary for official SDK compatibility.
	key, err := base64.StdEncoding.DecodeString(encodingAESKey + "=")
	if err != nil || len(key) != 32 {
		return nil, ErrCallbackCrypto
	}
	c := &CallbackCrypt{token: token, corpID: corpID}
	copy(c.key[:], key)
	return c, nil
}

// VerifyURL verifies the GET echostr challenge. The caller must URL-decode the
// query exactly once and return these bytes unchanged, without a newline/BOM.
// Freshness and replay protection belong to the receiving HTTP handler/store.
func (c *CallbackCrypt) VerifyURL(signature, timestamp, nonce, echo string) ([]byte, error) {
	return c.Decrypt(signature, timestamp, nonce, echo)
}

// Decrypt authenticates the ciphertext before any decryption, validates all
// PKCS#7 bytes (32-byte padding blocks), then validates the network-order length
// and CorpID. It never treats unauthenticated outer XML fields as identity.
func (c *CallbackCrypt) Decrypt(signature, timestamp, nonce, encrypted string) ([]byte, error) {
	if c == nil || c.token == "" || c.corpID == "" || !validCallbackMetadata(timestamp, nonce) || len(encrypted) == 0 || len(encrypted) > maxCallbackEncrypted || len(signature) != sha1.Size*2 {
		return nil, ErrCallbackCrypto
	}
	supplied, err := hex.DecodeString(signature)
	if err != nil {
		return nil, ErrCallbackCrypto
	}
	wanted, _ := hex.DecodeString(callbackSignature(c.token, timestamp, nonce, encrypted))
	if subtle.ConstantTimeCompare(supplied, wanted) != 1 {
		return nil, ErrCallbackCrypto
	}
	// DecodeString ignores CR/LF even in Strict mode. Reject them explicitly;
	// encrypted callback strings are one canonical base64 value, not MIME data.
	if strings.ContainsAny(encrypted, "\r\n") {
		return nil, ErrCallbackCrypto
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encrypted)
	if err != nil || len(decoded) < 32 || len(decoded)%32 != 0 {
		return nil, ErrCallbackCrypto
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, ErrCallbackCrypto
	}
	cipher.NewCBCDecrypter(block, c.key[:aes.BlockSize]).CryptBlocks(decoded, decoded)
	pad := int(decoded[len(decoded)-1])
	valid := subtle.ConstantTimeLessOrEq(1, pad) & subtle.ConstantTimeLessOrEq(pad, 32)
	// Inspect the whole last padding block irrespective of the claimed length.
	for i := 1; i <= 32; i++ {
		inPadding := subtle.ConstantTimeLessOrEq(i, pad)
		equal := subtle.ConstantTimeByteEq(decoded[len(decoded)-i], byte(pad))
		valid &= (1 - inPadding) | equal
	}
	if valid != 1 {
		return nil, ErrCallbackCrypto
	}
	decoded = decoded[:len(decoded)-pad]
	if len(decoded) < 20 {
		return nil, ErrCallbackCrypto
	}
	msgLen := uint64(binary.BigEndian.Uint32(decoded[16:20]))
	if msgLen > maxCallbackPlaintext || msgLen > uint64(len(decoded)-20) {
		return nil, ErrCallbackCrypto
	}
	end := 20 + int(msgLen)
	if subtle.ConstantTimeCompare(decoded[end:], []byte(c.corpID)) != 1 {
		return nil, ErrCallbackCrypto
	}
	return bytes.Clone(decoded[20:end]), nil
}

// EncryptReply encrypts the plaintext reply and returns the signed XML response
// envelope. It generates the required 16 random bytes using crypto/rand. The
// caller supplies the response timestamp/nonce and constructs the inner XML.
func (c *CallbackCrypt) EncryptReply(plaintext []byte, timestamp, nonce string) ([]byte, error) {
	return c.encryptReply(plaintext, timestamp, nonce, rand.Reader)
}

func (c *CallbackCrypt) encryptReply(plaintext []byte, timestamp, nonce string, random io.Reader) ([]byte, error) {
	if c == nil || c.token == "" || c.corpID == "" || !validCallbackMetadata(timestamp, nonce) || len(plaintext) > maxCallbackPlaintext {
		return nil, ErrCallbackCrypto
	}
	n := 20 + len(plaintext) + len(c.corpID)
	pad := 32 - n%32
	data := make([]byte, n+pad)
	if _, err := io.ReadFull(random, data[:16]); err != nil {
		return nil, ErrCallbackCrypto
	}
	binary.BigEndian.PutUint32(data[16:20], uint32(len(plaintext)))
	copy(data[20:], plaintext)
	copy(data[20+len(plaintext):], c.corpID)
	for i := n; i < len(data); i++ {
		data[i] = byte(pad)
	}
	block, err := aes.NewCipher(c.key[:])
	if err != nil {
		return nil, ErrCallbackCrypto
	}
	cipher.NewCBCEncrypter(block, c.key[:aes.BlockSize]).CryptBlocks(data, data)
	encrypted := base64.StdEncoding.EncodeToString(data)
	envelope := struct {
		XMLName      xml.Name `xml:"xml"`
		Encrypt      string   `xml:"Encrypt"`
		MsgSignature string   `xml:"MsgSignature"`
		TimeStamp    string   `xml:"TimeStamp"`
		Nonce        string   `xml:"Nonce"`
	}{Encrypt: encrypted, MsgSignature: callbackSignature(c.token, timestamp, nonce, encrypted), TimeStamp: timestamp, Nonce: nonce}
	out, err := xml.Marshal(envelope)
	if err != nil {
		return nil, ErrCallbackCrypto
	}
	return out, nil
}

func validCallbackMetadata(timestamp, nonce string) bool {
	if timestamp == "" || len(timestamp) > 20 || nonce == "" || len(nonce) > 256 {
		return false
	}
	for _, ch := range timestamp {
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func callbackSignature(token, timestamp, nonce, encrypted string) string {
	values := []string{token, timestamp, nonce, encrypted}
	sort.Strings(values)
	h := sha1.New()
	for _, value := range values {
		_, _ = io.WriteString(h, value)
	}
	return hex.EncodeToString(h.Sum(nil))
}
