package main

// Connection layer: credential → *redis.Client.
//
// **One client per credential, reused for the whole process**: go-redis already pools connections, and
// building a new client on every call would mean a TCP handshake + AUTH (+ TLS handshake) before every
// single command — under heavy write load that overhead would dwarf the commands themselves. The cache
// key is a fingerprint (sha256) of the credential contents, not the plaintext — a map key would show up
// in a crash dump.

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func credOf(ctx plugin.Ctx) Cred {
	var c Cred
	sokel.BindCredential(ctx, &c)
	return c
}

// redisClient is just an alias for *redis.Client, giving the cache and tests a short name.
type redisClient = redis.Client

var (
	poolMu sync.Mutex
	pool   = map[string]*redisClient{}
)

// optionsOf converts a credential into connection options. When the address has no port, 6379 is
// appended (a common shape when copying from a cloud console).
func optionsOf(c Cred) (*redis.Options, error) {
	addr := strings.TrimSpace(c.Addr)
	if addr == "" {
		return nil, fmt.Errorf("凭证缺地址——填 host:port（如 redis.internal:6379）")
	}
	addr = strings.TrimPrefix(strings.TrimPrefix(addr, "redis://"), "rediss://")
	addr = strings.TrimRight(addr, "/")
	if !strings.Contains(addr, ":") {
		addr += ":6379"
	}
	db := 0
	if s := strings.TrimSpace(c.DB); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("凭证里的库号 %q 不是合法的非负整数（默认 0）", c.DB)
		}
		db = n
	}
	opt := &redis.Options{
		Addr:         addr,
		Username:     strings.TrimSpace(c.Username),
		Password:     c.Password,
		DB:           db,
		DialTimeout:  10 * time.Second,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}
	switch strings.TrimSpace(c.TLS) {
	case "", "off":
	case "on":
		host, _, _ := strings.Cut(addr, ":")
		opt.TLSConfig = &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	case "on_insecure":
		// Self-hosted instances with a self-signed certificate: the connection is still encrypted, but
		// the peer's identity isn't verified — the credential UI spells this out explicitly.
		opt.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicitly chosen via the credential
	default:
		return nil, fmt.Errorf("凭证里的 TLS 取值 %q 不认识（off / on / on_insecure）", c.TLS)
	}
	return opt, nil
}

func fingerprint(o *redis.Options) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%v", o.Addr, o.Username, o.Password, o.DB, o.TLSConfig != nil)))
	return hex.EncodeToString(sum[:8])
}

// clientOf gets (or creates) the client for this credential.
func clientOf(c Cred) (*redis.Client, error) {
	opt, err := optionsOf(c)
	if err != nil {
		return nil, err
	}
	key := fingerprint(opt)
	poolMu.Lock()
	defer poolMu.Unlock()
	if cli, ok := pool[key]; ok {
		return cli, nil
	}
	cli := redis.NewClient(opt)
	pool[key] = cli
	return cli, nil
}

func clientFor(ctx plugin.Ctx) (*redis.Client, error) { return clientOf(credOf(ctx)) }

// pairs flattens a contract object field into go-redis's variadic argument form (k1, v1, k2, v2, …).
// Values are always written as strings: Redis itself only ever stores strings, and normalizing
// numbers/booleans here avoids a whole class of "it doesn't look the same coming out as it did going in"
// confusion.
func pairs(m map[string]any) []any {
	out := make([]any, 0, len(m)*2)
	for k, v := range m {
		out = append(out, k, asString(v))
	}
	return out
}

func asString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		// All JSON numbers go through here: integers shouldn't come out formatted as 1e+06.
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// strMap converts a map[string]string into a contract object field.
func strMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// splitList converts a comma-separated credential field into a list, trimming whitespace and dropping
// empty entries.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
