// platformStore：clawbot Store 接口的平台适配（P3，docs/plugin-multibot-qr-auth.md §2/§4）。
// 平台是唯一凭证存储方：
//   - LoadCredentials：读凭证行 fields.session（注册下发，SourceCtx.Credential）；
//   - SaveCredentials：经 sokel.credential.update 回写（SourceCtx.UpdateCredential）——运行中 token 刷新不丢；
//   - SyncBuf / ContextToken：进程内存（丢失代价小：sync 游标重置=从当下重新收；context token 随下一条
//     入站消息自然重建，仅影响重启后「先发消息」的能力）。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/importcjj/wechat-clawbot-client-go/store"
)

// sessionJSON：凭证行 fields.session 的载荷 = store.Credentials 的 JSON（不透明保存，格式随库演进）。
func sessionToJSON(c store.Credentials) string {
	b, _ := json.Marshal(c)
	return string(b)
}

func sessionFromJSON(s string) (store.Credentials, bool) {
	var c store.Credentials
	if s == "" || json.Unmarshal([]byte(s), &c) != nil || c.Token == "" {
		return store.Credentials{}, false
	}
	return c, true
}

type platformStore struct {
	mu      sync.Mutex
	creds   store.Credentials
	hasCred bool
	// 回写钩子：源实例 = SourceCtx.UpdateCredential；登录流（auth_start 的内存会话）= 捕获到 authSession。
	save func(sessionJSON string) error

	syncBuf string
	tokens  map[string]string // to → context token（内存；随入站消息重建）
}

func newPlatformStore(sessionJSON string, save func(string) error) *platformStore {
	ps := &platformStore{save: save, tokens: map[string]string{}}
	if c, ok := sessionFromJSON(sessionJSON); ok {
		ps.creds, ps.hasCred = c, true
	}
	return ps
}

func (p *platformStore) SaveCredentials(_ context.Context, _ string, creds store.Credentials) error {
	p.mu.Lock()
	p.creds, p.hasCred = creds, true
	p.mu.Unlock()
	if p.save != nil {
		return p.save(sessionToJSON(creds))
	}
	return nil
}

func (p *platformStore) LoadCredentials(_ context.Context, _ string) (store.Credentials, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.hasCred {
		return store.Credentials{}, fmt.Errorf("credentials not found") // 与库内 MemoryStore 同语义（无 sentinel error）
	}
	return p.creds, nil
}

func (p *platformStore) DeleteCredentials(_ context.Context, _ string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.creds, p.hasCred = store.Credentials{}, false
	return nil
}

func (p *platformStore) SaveSyncBuf(_ context.Context, _ string, buf string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.syncBuf = buf
	return nil
}

func (p *platformStore) LoadSyncBuf(_ context.Context, _ string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.syncBuf, nil
}

func (p *platformStore) SaveContextToken(_ context.Context, _ string, userID, token string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tokens[userID] = token
	return nil
}

func (p *platformStore) LoadContextToken(_ context.Context, _ string, userID string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.tokens[userID], nil
}
