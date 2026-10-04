// Package schema 声明 synology 插件的操作与事件契约。
//
// 事件与操作用同一套声明手法：类型上写方法，生成器读它。此前事件只能命令式声明
// （DeclareEvent[T] + 反射），这个插件因此一直迁不到声明式。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— 事件 ——
//
// 三个事件共享 path/name/ext/dir 四个公共字段（见 Events.CommonFields）：
// 平台把它们平铺到触发输入顶层，各分支共用同一变量。

// FileCreated 新文件落定后。
type FileCreated struct{}

func (FileCreated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_created", Label: "文件新增",
		Desc: "文件静默 settle 秒且大小稳定后才报——SMB 保存是临时文件+改名，大文件拷贝是持续写入，不等落定会拿到写了一半的文件"}
}
func (FileCreated) Fields() []contract.FieldSpec { return fileFields() }

// FileChanged 已有文件被改写并落定。
type FileChanged struct{}

func (FileChanged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_changed", Label: "文件修改"}
}
func (FileChanged) Fields() []contract.FieldSpec { return fileFields() }

// FileDeleted 文件被删除或移出监听树。
type FileDeleted struct{}

func (FileDeleted) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_deleted", Label: "文件删除"}
}

// 删除事件没有 size/mtime——文件已经不在了。
func (FileDeleted) Fields() []contract.FieldSpec { return commonFileFields() }

// Events 声明公共字段。
//
// 显式列出而不是从各事件推交集：推的话，将来新增一个事件少写了某字段，
// 公共字段就悄悄缩水、存量工作流跟着断，而那时没人会想到是这里。
type Events struct{}

func (Events) CommonFields() []string { return []string{"path", "name", "ext", "dir"} }

func commonFileFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("path").Label("完整路径"),
		field.String("name").Label("文件名"),
		field.String("ext").Label("扩展名"),
		field.String("dir").Label("所在目录"),
	}
}

func fileFields() []contract.FieldSpec {
	return append(commonFileFields(),
		field.Int("size").Label("大小（字节）"),
		field.String("mtime").Label("修改时间（RFC3339）"),
	)
}

// —— 操作 ——

// ReadFile 按路径读取监听目录内的文件。
type ReadFile struct{}

func (ReadFile) Meta() contract.Meta {
	return contract.Meta{ID: "read_file", Label: "读取文件",
		Desc: "按路径读取监听目录内的文件，产出平台文件引用（下游文件参数可用）"}
}
func (ReadFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("path").Label("文件路径").Desc("绝对路径或相对监听目录；须在凭证监听目录内"),
	}
}
func (ReadFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.File("file").Label("文件"),
		field.String("name").Label("文件名"),
		field.Int("size").Label("大小（字节）"),
		field.String("mtime").Label("修改时间（RFC3339）"),
	}
}

// ListDir 列出监听目录内某子目录的条目。
type ListDir struct{}

func (ListDir) Meta() contract.Meta {
	return contract.Meta{ID: "list_dir", Label: "列出目录", Desc: "列出监听目录内某子目录的条目（忽略系统垃圾目录）"}
}
func (ListDir) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("dir").Label("目录").Desc("相对监听目录；留空=监听目录本身").Optional(),
	}
}
func (ListDir) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("entries", []DirEntry{}).Label("条目"),
		field.Int("count").Label("数量"),
	}
}

// DirEntry 目录里的一个条目。
type DirEntry struct {
	Name  string `sokel:"name" label:"名称"`
	Path  string `sokel:"path" label:"完整路径"`
	IsDir bool   `sokel:"is_dir" label:"是否目录"`
	Size  int64  `sokel:"size" label:"大小（字节）"`
	Mtime string `sokel:"mtime" label:"修改时间"`
}

// MoveFile 监听目录内移动/改名（处理完归档的典型用法）。
type MoveFile struct{}

func (MoveFile) Meta() contract.Meta {
	return contract.Meta{ID: "move_file", Label: "移动文件",
		Desc: "监听目录内移动/改名文件（处理完归档的典型用法），父目录自动创建"}
}
func (MoveFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("path").Label("源文件").Desc("要移动的文件（须在监听目录内）"),
		field.String("to").Label("目标").
			Desc("目标目录（以 / 结尾或已存在的目录 → 保留原名）或完整目标路径；须在监听目录内，父目录自动创建"),
	}
}
func (MoveFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("是否成功"),
		field.String("to").Label("最终目标路径"),
	}
}

// DeleteFile 删除监听目录内的一个文件。
type DeleteFile struct{}

func (DeleteFile) Meta() contract.Meta {
	return contract.Meta{ID: "delete_file", Label: "删除文件", Desc: "删除监听目录内的一个文件（不删目录）"}
}
func (DeleteFile) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("path").Label("文件路径").Desc("要删除的文件（不删目录）；须在监听目录内"),
	}
}
func (DeleteFile) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("是否成功")}
}

// —— 凭证 ——

// Credential 凭证契约（值均为字符串，注册时自报 schema）。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("path").Label("监听目录").
			Desc("容器内路径（挂载点下子目录），如 /watch/研报入库"),
		field.Text("include").Label("扩展名过滤").
			Desc("逗号分隔（pdf,docx）；留空=全部文件").Optional(),
		field.Text("ignore").Label("忽略模式").
			Desc("逗号分隔子串，命中即忽略；Synology 垃圾目录（@eaDir/#recycle 等）恒忽略").Optional(),
		field.Text("settle_seconds").Label("落定秒数").
			Desc("文件静默 N 秒且大小稳定才上报（防写一半的文件）").Default("2"),
	}
}

// HealthCheck 平台约定的凭证体检（操作 id 必须是 health_check，凭证页的「检查」按钮据此验活）。
//
// 这个插件的「上游」就是挂进容器的那个目录——它不连 DSM API，凭证也不是账号密码，
// 而是一条容器内路径（见包头部署说明）。所以体检问的正是它唯一会错的那件事：
// 卷挂上了吗、路径拼对了吗、容器用户读得了吗。这三样错任何一样，表现都是
// 「事件源一直不出事件」——一种极难自证的静默故障，正该由体检当场说破。
//
// 出参 ok=false + message 表示不可用（不抛错——抛错在界面上只剩一个红叉，说不出是哪一样错了）。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "看监听目录在不在、是不是目录、容器读不读得了，并报当前条目数"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("path").Label("监听目录").Optional(),
		field.Int("entries").Label("当前条目数").Optional().Desc("目录第一层里未被忽略的条目"),
		field.String("message").Label("说明"),
	}
}
