// Package schema declares the operation and event contracts for the synology plugin.
//
// Events and operations use the same declarative approach: methods written on a type,
// read by the generator. Events used to only support imperative declaration
// (DeclareEvent[T] + reflection), which is why this plugin had been stuck unable to move
// to the declarative style.
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— events ——
//
// The three events share four common fields, path/name/ext/dir (see
// Events.CommonFields): the platform flattens them into the top level of the trigger
// input, so each branch shares the same variables.

// FileCreated fires once a new file has settled.
type FileCreated struct{}

func (FileCreated) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_created", Label: "文件新增",
		Desc: "文件静默 settle 秒且大小稳定后才报——SMB 保存是临时文件+改名，大文件拷贝是持续写入，不等落定会拿到写了一半的文件"}
}
func (FileCreated) Fields() []contract.FieldSpec { return fileFields() }

// FileChanged fires when an existing file is rewritten and has settled.
type FileChanged struct{}

func (FileChanged) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_changed", Label: "文件修改"}
}
func (FileChanged) Fields() []contract.FieldSpec { return fileFields() }

// FileDeleted fires when a file is deleted or moved out of the watched tree.
type FileDeleted struct{}

func (FileDeleted) EventMeta() contract.EventMeta {
	return contract.EventMeta{ID: "file_deleted", Label: "文件删除"}
}

// The delete event has no size/mtime -- the file is already gone.
func (FileDeleted) Fields() []contract.FieldSpec { return commonFileFields() }

// Events declares the common fields.
//
// Listed explicitly rather than inferred as the intersection across events: inferring it
// would mean that if a future event is added missing some field, the common fields would
// silently shrink and existing workflows would break along with it, with no one thinking
// to look here at that point.
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

// —— operations ——

// ReadFile reads a file inside the watched directory by path.
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

// ListDir lists the entries of a subdirectory inside the watched directory.
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

// DirEntry is one entry inside a directory.
type DirEntry struct {
	Name  string `sokel:"name" label:"名称"`
	Path  string `sokel:"path" label:"完整路径"`
	IsDir bool   `sokel:"is_dir" label:"是否目录"`
	Size  int64  `sokel:"size" label:"大小（字节）"`
	Mtime string `sokel:"mtime" label:"修改时间"`
}

// MoveFile moves/renames a file inside the watched directory (the typical use case being archiving after processing).
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

// DeleteFile deletes one file inside the watched directory.
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

// —— credential ——

// Credential is the credential contract (all values are strings; the schema is self-reported at registration).
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

// HealthCheck is the platform's standard credential health check (the operation id must
// be health_check; the credential page's "Check" button relies on this to verify
// liveness).
//
// This plugin's "upstream" is just the directory mounted into the container -- it
// doesn't connect to a DSM API, and the credential isn't a username/password either, but
// a path inside the container (see the package-level deployment notes). So the health
// check asks exactly the one thing that can go wrong: is the volume mounted, is the path
// correct, can the container user read it. Any one of these three being wrong looks the
// same from outside -- "the event source just never fires anything" -- an extremely hard
// failure to diagnose on your own, which is exactly what the health check should expose
// on the spot.
//
// ok=false + message in the output means unavailable (not an error -- an error would
// leave nothing but a red X in the UI, with no way to tell which of the three went wrong).
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
