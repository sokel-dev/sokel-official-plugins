// Package schema declares the submail plugin's operation and credential contracts.
//
// Scope: an SMS gateway -- domestic SMS + international SMS (SUBMAIL cloud communications).
// Alert notifications, verification codes, and marketing outreach all go through it. A plain
// HTTP form API, no SDK needed.
//
// Two of SUBMAIL's own platform conventions shape the contract:
//
//   - Apps are typed: the domestic SMS app and the international SMS app are two separate apps
//     in the console, with two separate appid/appkey pairs. So the credential has two groups of
//     fields, both optional -- using only domestic doesn't require configuring international,
//     and vice versa; a call missing the needed group gets a pointed error.
//   - Sending splits into "full text" and "template" paths: send gives the content directly
//     (domestic requires a registered 【signature】), xsend gives a template id + variables (the
//     template must be reviewed in the console first). Verification-code templates get
//     reviewed quickly with a stable delivery rate; marketing content is mostly sent as full
//     text -- both paths are supported.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// SmsSend sends domestic SMS as full text.
type SmsSend struct{}

func (SmsSend) Meta() contract.Meta {
	return contract.Meta{ID: "sms_send", Label: "国内短信·发送",
		Desc: "全文发送：内容必须带已报备的短信签名（如【某某科技】），否则运营商直接拒收"}
}

func (SmsSend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("手机号").Desc("11 位号码；多个逗号分隔（单次上限 1 万）"),
		field.Text("content").Label("内容").Desc("**必须含【已报备的签名】**，通常放开头；70 字以内一条，超出按 67 字/条分段计费"),
	}
}

func (SmsSend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("send_id").Label("发送 ID"),
		field.Int("fee").Label("计费条数"),
	}
}

// SmsXsend sends domestic SMS from a template.
type SmsXsend struct{}

func (SmsXsend) Meta() contract.Meta {
	return contract.Meta{ID: "sms_xsend", Label: "国内短信·模板发送",
		Desc: "按已审核的模板发送（验证码/通知类推荐）：给模板 ID + 变量"}
}

func (SmsXsend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("手机号").Desc("11 位号码；多个逗号分隔"),
		field.String("project").Label("模板 ID").Desc("控制台「短信模板」里的 项目标识（如 abc12）"),
		field.Object("vars", "模板变量对象，键=模板里的 @var(键名)；本插件原样传给 SUBMAIL").
			Label("模板变量").Desc("如 {\"code\":\"123456\"}").Optional(),
	}
}

func (SmsXsend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("send_id").Label("发送 ID"),
		field.Int("fee").Label("计费条数"),
	}
}

// IntlSend sends international SMS as full text.
type IntlSend struct{}

func (IntlSend) Meta() contract.Meta {
	return contract.Meta{ID: "intl_send", Label: "国际短信·发送",
		Desc: "发到境外号码：号码带国家码（如 +81…、+1…）。用凭证里的国际短信应用"}
}

func (IntlSend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("手机号").Desc("带国家码：+81xxx / +1xxx；多个逗号分隔"),
		field.Text("content").Label("内容").Desc("英文 160 字符一条；中文等 Unicode 70 字符一条，超出分段计费"),
	}
}

func (IntlSend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("send_id").Label("发送 ID"),
		field.Int("fee").Label("计费条数"),
	}
}

// IntlXsend sends international SMS from a template.
type IntlXsend struct{}

func (IntlXsend) Meta() contract.Meta {
	return contract.Meta{ID: "intl_xsend", Label: "国际短信·模板发送",
		Desc: "按已审核的国际短信模板发送：模板 ID + 变量"}
}

func (IntlXsend) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("to").Label("手机号").Desc("带国家码；多个逗号分隔"),
		field.String("project").Label("模板 ID"),
		field.Object("vars", "模板变量对象，键=模板里的 @var(键名)；原样传给 SUBMAIL").
			Label("模板变量").Optional(),
	}
}

func (IntlXsend) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("send_id").Label("发送 ID"),
		field.Int("fee").Label("计费条数"),
	}
}

// Balance checks the balance. Domestic and international are two endpoints with two different
// units (domestic by message count, international by amount); whichever app group is
// configured gets queried, and everything is returned at once.
type Balance struct{}

func (Balance) Meta() contract.Meta {
	return contract.Meta{ID: "balance", Label: "查余额",
		Desc: "查短信余额：国内（条数）+ 国际（余额，按金额计费），配了哪组应用查哪边。" +
			"余额告警工作流用：定时查 → 低于阈值发通知"}
}

func (Balance) Inputs() []contract.FieldSpec { return nil }

func (Balance) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Int("balance").Label("国内·运营类余额(条)").Desc("没配国内应用时为 0"),
		field.Int("transactional").Label("国内·三网合一余额(条)"),
		field.Number("intl_balance").Label("国际·余额").Desc("国际短信按金额计费；没配国际应用时为 0"),
	}
}

// SmsLog checks a domestic SMS's delivery status -- the self-service troubleshooting entry
// point for "why didn't it arrive". SUBMAIL accepting the submission only means it entered the
// queue; whether it reached the phone requires checking the delivery status here.
// International SMS has no corresponding query endpoint (returns Unknown Method in practice);
// delivery status there can only be viewed in the console.
type SmsLog struct{}

func (SmsLog) Meta() contract.Meta {
	return contract.Meta{ID: "sms_log", Label: "国内短信·查发送状态",
		Desc: "按 send_id 或手机号查下发结果：delivered=已送达，dropped=运营商丢弃（签名未报备/内容风控/空号）。" +
			"发送成功但手机没响，答案在这里"}
}

func (SmsLog) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("send_id").Label("发送 ID").Desc("发送操作的产出；与手机号至少填一个").Optional(),
		field.String("to").Label("手机号").Desc("查这个号码最近的下发记录").Optional(),
		field.Int("days").Label("查最近几天").Desc("默认 1（今天）").Optional(),
	}
}

func (SmsLog) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("logs", []map[string]any{}).Label("发送记录").
			Desc("每条含 send_id/to/status/report 等，形状由 SUBMAIL 定义——status: delivered=已送达 dropped=被丢弃（report 里有运营商原因）sending=在途"),
		field.Int("count").Label("条数"),
	}
}

// HealthCheck is the platform's standard credential health check.
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "用配好的应用各查一次余额——appid/appkey 有误当场暴露"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("message").Label("说明"),
	}
}

// Credential: domestic and international are two separate apps with two separate key pairs
// (created separately in the SUBMAIL console), both optional -- configure only the side you
// use; a call missing the needed group gets a pointed error.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("sms_appid").Label("国内短信 AppID").
			Desc("控制台「短信」应用的 AppID（纯数字）。只用国际短信可留空").Optional(),
		field.Secret("sms_appkey").Label("国内短信 AppKey").Optional(),
		field.Text("intl_appid").Label("国际短信 AppID").
			Desc("控制台「国际短信」应用的 AppID。只用国内短信可留空").Optional(),
		field.Secret("intl_appkey").Label("国际短信 AppKey").Optional(),
	}
}
