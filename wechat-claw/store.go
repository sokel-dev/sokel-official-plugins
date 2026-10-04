// platformStore is the platform adapter for clawbot's Store interface (P3,
// docs/plugin-multibot-qr-auth.md §2/§4). The platform is the sole credential store:
//   - LoadCredentials: reads the credential row's fields.session (issued at registration,
//     SourceCtx.Credential);
//   - SaveCredentials: writes back via sokel.credential.update (SourceCtx.UpdateCredential) — a
//     token refresh while running isn't lost;
//   - SyncBuf / ContextToken: process memory (cheap to lose: the sync cursor just resets to
//     resume from now; the context token naturally rebuilds from the next inbound message, only
//     affecting the "send a message first" ability right after a restart).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/importcjj/wechat-clawbot-client-go/store"
)

// sessionJSON: the credential row's fields.session payload = the JSON of store.Credentials
// (saved opaquely; its shape evolves with the upstream library).
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
	// Write-back hook: for a source instance = SourceCtx.UpdateCredential; for the login flow
	// (auth_start's in-memory session) = captured into authSession.
	save func(sessionJSON string) error

	syncBuf string
	tokens  map[string]string // to → context token (in memory; rebuilt from inbound messages)
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
		return store.Credentials{}, fmt.Errorf("credentials not found") // same semantics as the library's own MemoryStore (no sentinel error)
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
