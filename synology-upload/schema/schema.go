// Package schema declares the operation and credential contracts for the synology-upload plugin.
//
// Why a plugin of its own instead of a write operation on the synology plugin: that plugin works on a NAS directory
// bind-mounted into its container, and a file a container writes into a bind mount never reaches Synology Drive /
// ShareSync — the host gets no change notification for it, so the file sits on disk but never syncs (until something
// on the host touches it or Drive restarts). Uploading through DSM's own FileStation API writes the file as DSM, so
// Drive, Cloud Sync and indexing all see it. That needs a DSM account rather than a mounted path, and can run anywhere
// the NAS is reachable — a different credential and deployment shape, hence a different plugin.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// UploadFile uploads files into a NAS folder.
type UploadFile struct{}

func (UploadFile) Meta() contract.Meta {
	return contract.Meta{ID: "upload_file", Label: "上传文件",
		Desc: "经 DSM 的 FileStation 接口把文件传到群晖某个文件夹，Synology Drive / Cloud Sync 能照常感知并同步", TimeoutSec: 600}
}

func (UploadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Files("files").Label("文件").Desc("一个或多个平台文件，按原文件名存放"),
		field.String("dir").Label("目标文件夹").
			Desc("DSM 里看到的路径，以共享文件夹开头，如 /research/入库/2026（不是容器或 SSH 里的 /volume1/...）"),
		field.Enum("on_conflict", field.Opt("error", "报错"), field.Opt("overwrite", "覆盖"), field.Opt("skip", "跳过")).
			Label("同名文件").Desc("目标文件夹里已有同名文件时怎么办；跳过的文件在结果里标出").Default("error"),
		field.Bool("create_parents").Label("自动创建文件夹").Desc("目标文件夹不存在时逐级创建").Default(true),
	}
}

func (UploadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("files", []Uploaded{}).Label("结果").Desc("与输入的文件一一对应"),
		field.Int("uploaded").Label("上传数"),
		field.Int("skipped").Label("跳过数").Desc("「同名文件」选跳过时，已存在而没有上传的数量"),
	}
}

// Uploaded is one file's result.
type Uploaded struct {
	Name    string `sokel:"name" label:"文件名"`
	Path    string `sokel:"path" label:"NAS 路径"`
	Size    int64  `sokel:"size" label:"大小（字节）"`
	Skipped bool   `sokel:"skipped" label:"已跳过"`
}

// HealthCheck checks the credential (the platform's standard operation id).
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "用账号密码登录一次 DSM。ok=false 时 message 说明是地址不通、证书、密码还是两步验证的问题"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("凭证可用"),
		field.String("message").Label("说明"),
	}
}

// Credential is the credential contract.
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("url").Label("DSM 地址").
			Desc("浏览器里打开 DSM 的地址，带协议和端口，如 https://nas.example.com:5001 或 http://nas.local:5000"),
		field.Text("username").Label("账号").
			Desc("建议单独建一个只对目标共享文件夹有读写权限的账号；**不能开两步验证**（接口登录没法输验证码）"),
		field.Secret("password").Label("密码"),
		field.Text("insecure_tls").Label("跳过证书校验").
			Desc("填 true 时不校验 HTTPS 证书，仅用于局域网里 DSM 的自签证书；走公网请保持留空").Optional(),
	}
}
