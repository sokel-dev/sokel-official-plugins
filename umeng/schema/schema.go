// Package schema declares the contract for the umeng (Umeng push / U-Push) plugin.
//
// Scope: the other half of the app push channel — the push architecture is Aliyun Push
// plus Umeng running **concurrently on two channels** (following the existing shape of
// the referenced push service); the Aliyun half lives in the aliyun plugin, this one
// fills in Umeng.
//
// Umeng's platform conventions shape the contract: **Android and iOS are two separate
// apps in Umeng** (each with its own appkey/master secret pair), and the payload shapes
// are also completely different (Android has its own format, iOS uses the APNs aps
// structure) — so the credential's four fields are grouped by platform, and operations
// select the platform.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Push sends a notification.
type Push struct{}

func (Push) Meta() contract.Meta {
	return contract.Meta{ID: "push", Label: "推送",
		Desc: "友盟 U-Push：按 device_token 单播/列播（≤500 个），或全量广播。" +
			"Android 与 iOS 各用各的友盟 App（凭证里按平台配）"}
}

func (Push) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("platform", field.Opt("android", "Android"), field.Opt("ios", "iOS")).
			Label("平台").Default("android"),
		field.String("device_tokens").Label("设备 token").
			Desc("多个逗号分隔（≤500）；**留空 = 全量广播**，想清楚再发").Optional(),
		field.String("title").Label("标题"),
		field.Text("body").Label("内容"),
		field.Object("extras", "自定义键值对，客户端点开通知时拿到；形状由你的 App 约定").
			Label("附加参数").Optional(),
		field.Bool("production").Label("生产模式").
			Desc("iOS 区分生产/测试证书环境；Android 测试模式只发给测试设备").Default(true),
	}
}

func (Push) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("task_id").Label("任务 ID").
			Desc("广播/列播回任务 ID（可查状态/撤销）；单播是消息类，回 msg_id，无任务统计"),
	}
}

// TaskStatus queries a task's status.
type TaskStatus struct{}

func (TaskStatus) Meta() contract.Meta {
	return contract.Meta{ID: "task_status", Label: "查任务状态",
		Desc: "查广播/列播任务的下发进度（单播消息类没有任务统计——友盟的约定）"}
}

func (TaskStatus) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("platform", field.Opt("android", "Android"), field.Opt("ios", "iOS")).
			Label("平台").Default("android"),
		field.String("task_id").Label("任务 ID"),
	}
}

func (TaskStatus) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("status").Label("状态码").Desc("0排队 1发送中 2完成 3失败 4撤销 5过期 6筛选为空 7定时未开始"),
		field.String("status_text").Label("状态"),
		field.Int("sent_count").Label("下发数"),
		field.Int("open_count").Label("打开数"),
	}
}

// Cancel cancels a task.
type Cancel struct{}

func (Cancel) Meta() contract.Meta {
	return contract.Meta{ID: "cancel", Label: "撤销任务",
		Desc: "撤销**尚未发送**的任务类消息（定时任务/排队中）；已下发的收不回"}
}

func (Cancel) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Enum("platform", field.Opt("android", "Android"), field.Opt("ios", "iOS")).
			Label("平台").Default("android"),
		field.String("task_id").Label("任务 ID"),
	}
}

func (Cancel) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// HealthCheck is the platform-conventional credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "用配好的平台各查一次（拿一个不存在的任务号问状态）——钥匙错当场暴露，不会真推送"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// Credential: Android and iOS are two separate apps in Umeng, so there are two key
// groups by platform, both optional.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("android_app_key").Label("Android AppKey").
			Desc("友盟控制台 Android 应用的 AppKey。只推 iOS 可留空").Optional(),
		field.Secret("android_master_secret").Label("Android Master Secret").Optional(),
		field.Text("ios_app_key").Label("iOS AppKey").Optional(),
		field.Secret("ios_master_secret").Label("iOS Master Secret").Optional(),
	}
}
