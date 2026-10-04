// Package schema 声明 umeng（友盟推送 U-Push）插件的契约。
//
// 定位：App 推送的另一半通道——你的推送架构是阿里云推送 + 友盟**双通道并发**
// （参照的推送服务的既有形态），阿里那半在 aliyun 插件里，这里补友盟。
//
// 友盟的平台约定决定契约形状：**Android 与 iOS 在友盟里是两个 App**（各一对
// appkey/master secret），payload 形状也完全不同（Android 自有格式，iOS 是 APNs
// aps 结构）——所以凭证四个字段按平台分组，操作里选平台。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// Push 推送。
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

// TaskStatus 查任务状态。
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

// Cancel 撤销任务。
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

// HealthCheck 平台约定的凭证体检。
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

// Credential：Android 与 iOS 是友盟里的两个 App，按平台两组钥匙，都选填。
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
