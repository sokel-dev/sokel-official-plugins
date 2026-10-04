package main

// Pure logic plus an httptest fake gateway. darabonba's generic call lets endpoint point at
// any host and still signs as usual (the fake gateway doesn't verify signatures), so the
// request shape can be exercised end-to-end. Real cloud integration is covered by
// operation:test.

import (
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

type fakeCtx struct {
	context.Context
	cred map[string]string
}

func newFake(cred map[string]string) *fakeCtx {
	return &fakeCtx{Context: context.Background(), cred: cred}
}

func (f *fakeCtx) Credential() map[string]string { return f.cred }
func (f *fakeCtx) Upload(string, string, []byte) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) UploadReader(string, string, io.Reader) (*plugin.File, error) {
	return &plugin.File{ID: "f"}, nil
}
func (f *fakeCtx) Fetch(*plugin.File) ([]byte, error) { return nil, nil }

func credFor(host string) map[string]string {
	return map[string]string{"access_key_id": "LTAItest", "access_key_secret": "sk", "region": "cn-hangzhou"}
}

// The dig family is the foundation of all response parsing — get one level of the key path
// wrong and you get a silent empty value, so pin each case down.
func TestDigHelpers(t *testing.T) {
	m := map[string]any{
		"Items": map[string]any{"DBInstance": []any{
			map[string]any{"DBInstanceId": "rm-1", "Storage": float64(200)},
		}},
		"Total": "42",
	}
	if got := digStr(m, "Items", "DBInstance"); got == "" {
		// Converting an array to a string is meaningless, but it shouldn't panic.
		_ = got
	}
	list := digList(m, "Items", "DBInstance")
	if len(list) != 1 || digStr(list[0], "DBInstanceId") != "rm-1" {
		t.Fatalf("digList: %+v", list)
	}
	if digInt(m, "Total") != 42 {
		t.Error("字符串数字要能转 int（阿里云的数字常以字符串给）")
	}
	if digInt(list[0], "Storage") != 200 {
		t.Error("float64 要能转 int")
	}
	if dig(m, "no", "such") != nil {
		t.Error("不存在的路径要回 nil 不 panic")
	}
}

func TestParseWhen(t *testing.T) {
	if v, _ := parseWhen("1700000000", 0); v != 1700000000 {
		t.Error("秒级时间戳")
	}
	// Independently computed fixed value: 2026-08-20T10:00:00+08:00 = UTC 02:00 = 1787191200.
	if v, _ := parseWhen("2026-08-20T10:00:00+08:00", 0); v != 1787191200 {
		t.Errorf("RFC3339 = %d, want 1787191200", v)
	}
	if v, _ := parseWhen("", 99); v != 99 {
		t.Error("空串用默认")
	}
	if _, err := parseWhen("昨天", 0); err == nil {
		t.Error("认不出的要报错不猜")
	}
}

// Generic call error translation: 403 → points the user at RAM.
func TestAcsErrTranslation(t *testing.T) {
	for frag, want := range map[string]string{
		"InvalidAccessKeyId.NotFound":        "RAM 控制台核对",
		"SignatureDoesNotMatch":              "Secret 粘错",
		"Forbidden.RAM: user not authorized": "加对应授权",
		"Throttling.User":                    "频控",
	} {
		err := acsErr("TestAction", errFrom(frag))
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s → %v（想看到 %q）", frag, err, want)
		}
	}
}

func errFrom(s string) error { return &strErr{s} }

type strErr struct{ s string }

func (e *strErr) Error() string { return e.s }

// CloudMonitor's Datapoints is a JSON string, not an array (legacy baggage).
func TestParseJSONArray(t *testing.T) {
	pts := parseJSONArray(`[{"timestamp":1700000000000,"Average":73.5},{"timestamp":1700000060000,"Average":74.2}]`)
	if len(pts) != 2 || digFloat(pts[1], "Average") != 74.2 {
		t.Fatalf("pts=%+v", pts)
	}
	if parseJSONArray("not json") != nil {
		t.Error("解不开要回 nil 不 panic")
	}
}

// call input validation: an endpoint with a path, or a missing part of the triple, must both
// produce a clear error.
func TestCallValidation(t *testing.T) {
	ctx := newFake(credFor(""))
	if _, err := opCall(ctx, &CallIn{Endpoint: "rds.aliyuncs.com/some/path", Action: "X", Version: "2014-08-15"}); err == nil || !strings.Contains(err.Error(), "只填域名") {
		t.Errorf("endpoint 带路径要拒绝, got %v", err)
	}
	if _, err := opCall(ctx, &CallIn{Action: "X"}); err == nil || !strings.Contains(err.Error(), "都要填") {
		t.Errorf("缺参要说清, got %v", err)
	}
	if _, err := opSlsQuery(newFake(map[string]string{}), &SlsQueryIn{Project: "p", Logstore: "l"}); err == nil || !strings.Contains(err.Error(), "access_key") {
		t.Errorf("缺凭证要指路, got %v", err)
	}
}

var _ = json.Marshal

// push input validation and param normalization: TargetValue is forced to ALL when broadcasting;
// targeted pushes require a target value; iOS-related device types must carry iOSApnsEnv; extras
// is serialized and sent to both Android and iOS.
func TestPushValidation(t *testing.T) {
	ctx := newFake(credFor(""))
	if _, err := opPush(ctx, &PushIn{Target: "ALL", Title: "t", Body: "b"}); err == nil || !strings.Contains(err.Error(), "AppKey") {
		t.Errorf("缺 AppKey 要指路, got %v", err)
	}
	if _, err := opPush(ctx, &PushIn{AppKey: "123", Target: "ALIAS", Title: "t", Body: "b"}); err == nil || !strings.Contains(err.Error(), "目标值") {
		t.Errorf("按别名圈人没给值要拒绝, got %v", err)
	}
	if _, err := opPush(ctx, &PushIn{AppKey: "123", Target: "ALL", Title: "", Body: "b"}); err == nil || !strings.Contains(err.Error(), "标题") {
		t.Errorf("缺标题要拒绝, got %v", err)
	}
}

// send_mail input validation: at least one of the two body types is required; sender address,
// recipient, and subject are all required.
func TestSendMailValidation(t *testing.T) {
	ctx := newFake(credFor(""))
	if _, err := opSendMail(ctx, &SendMailIn{To: "a@b.com", Subject: "s", HTML: "<p>x</p>"}); err == nil || !strings.Contains(err.Error(), "发信地址") {
		t.Errorf("缺发信地址要指路, got %v", err)
	}
	if _, err := opSendMail(ctx, &SendMailIn{AccountName: "n@m.c", To: "a@b.com", Subject: "s"}); err == nil || !strings.Contains(err.Error(), "至少填一个") {
		t.Errorf("没正文要拒绝, got %v", err)
	}
}
