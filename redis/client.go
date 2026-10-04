package main

// 连接层：凭证 → *redis.Client。
//
// **一份凭证一个 client，全进程复用**：go-redis 自带连接池，而每次调用新建 client
// 等于每条命令前先做一次 TCP 握手 + AUTH（+ TLS 握手），密集写入时开销比命令本身还大。
// 缓存键是凭证内容的指纹（sha256），不是明文——map 的键会在崩溃转储里露脸。

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

// redisClient 只是 *redis.Client 的别名，给缓存与测试一个短名字。
type redisClient = redis.Client

var (
	poolMu sync.Mutex
	pool   = map[string]*redisClient{}
)

// optionsOf 凭证 → 连接参数。地址不带端口时补 6379（云控制台复制出来的常见形态）。
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
		// 自签证书的自建实例：连接仍加密，但不校验对端身份——凭证里把这件事说清楚了。
		opt.TLSConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // 凭证显式选择
	default:
		return nil, fmt.Errorf("凭证里的 TLS 取值 %q 不认识（off / on / on_insecure）", c.TLS)
	}
	return opt, nil
}

func fingerprint(o *redis.Options) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d|%v", o.Addr, o.Username, o.Password, o.DB, o.TLSConfig != nil)))
	return hex.EncodeToString(sum[:8])
}

// clientOf 取（或建）该凭证的 client。
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

// pairs 把契约里的对象字段摊平成 go-redis 的可变参数（k1, v1, k2, v2, …）。
// 值统一按字符串写入：Redis 侧本来就只存字符串，数字/布尔在这里定型能少一类
// 「取出来和存进去长得不一样」的困惑。
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
		// JSON 数字统一走这里：整数别打成 1e+06。
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

// strMap map[string]string → 契约的对象字段。
func strMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// splitList 逗号分隔的凭证字段 → 去空白去空项。
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
