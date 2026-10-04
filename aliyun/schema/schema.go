// Package schema 声明 aliyun 插件的操作与凭证契约。
//
// 定位：阿里云**管控面**一网打尽——SLS 日志、RDS、DNS、ACK、云监控 + call 保底。
// 集群内的工作负载操作（查 pod/看日志/重启 deployment）不在这里：那是 K8s API 的事，
// 用通用 kubernetes 插件（凭证 = kubeconfig，本插件的 ack_kubeconfig 能导出来喂它）。
//
// 一个别家没有的红利决定了本插件的形状：**阿里云 OpenAPI 是统一网关**——一套签名、
// {Action, Version, Endpoint} 三元组能调任意 RPC 产品。所以 call 保底操作天然覆盖
// 全部产品线（ECS/SLB/OSS 管控/…），typed 操作只是把高频的拼好参数。
// 唯一例外是 SLS：独立协议独立签名，走官方 aliyun-log-go-sdk。
//
// 安全边界：权限控制点在**阿里云 RAM**，插件不自造权限系统——凭证建议用最小权限的
// RAM 用户（docs 有现成策略 JSON）。高危写操作（RDS 重启/删实例/删集群）**不做 typed**，
// call 能调但要显式拼参数，有意抬高门槛。
package schema

import (
	"github.com/sokel-dev/sokel-plugin-sdk/contract"
	"github.com/sokel-dev/sokel-plugin-sdk/contract/field"
)

// —— SLS 日志 ——

// SlsQuery 查日志。
type SlsQuery struct{}

func (SlsQuery) Meta() contract.Meta {
	return contract.Meta{ID: "sls_query", Label: "SLS·查日志", TimeoutSec: 60,
		Desc: "按查询语句查一段时间的日志（支持 SLS 查询语法与 SQL92 分析，如 level:ERROR | select count(*)）"}
}

func (SlsQuery) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("project").Label("Project"),
		field.String("logstore").Label("Logstore"),
		field.String("query").Label("查询语句").
			Desc("SLS 查询语法；带 | 进入 SQL 分析（如 level:ERROR | SELECT count(*) AS c）").Optional(),
		field.Int("minutes").Label("查最近几分钟").Desc("默认 15；与 from/to 二选一").Optional(),
		field.String("from").Label("开始时间").Desc("RFC3339 或秒级时间戳；填了它就不看 minutes").Optional(),
		field.String("to").Label("结束时间").Desc("RFC3339 或秒级时间戳；默认现在").Optional(),
		field.Int("limit").Label("最多几条").Desc("默认 100，上限 1000（分析型查询不受此限）").Optional(),
	}
}

func (SlsQuery) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("logs", []map[string]any{}).Label("日志").
			Desc("每行是键值对象，键由日志字段决定；分析型查询则是聚合结果行，键由 SELECT 决定——形状随查询而变，无法预声明"),
		field.Int("count").Label("条数"),
		field.Bool("complete").Label("查询完整").Desc("false = 结果被截断或查询超时，缩小时间范围重试"),
	}
}

// SlsListLogstores 列出 project 下的 logstore。
type SlsListLogstores struct{}

func (SlsListLogstores) Meta() contract.Meta {
	return contract.Meta{ID: "sls_list_logstores", Label: "SLS·列 Logstore"}
}

func (SlsListLogstores) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("project").Label("Project")}
}

func (SlsListLogstores) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Array("logstores", []string{}).Label("Logstore 列表")}
}

// —— RDS ——

// RdsInstances 实例列表。
type RdsInstances struct{}

func (RdsInstances) Meta() contract.Meta {
	return contract.Meta{ID: "rds_instances", Label: "RDS·实例列表",
		Desc: "列出 region 内的 RDS 实例（状态/引擎/规格）"}
}

func (RdsInstances) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("region").Label("Region").Desc("留空用凭证里的默认 region").Optional(),
	}
}

func (RdsInstances) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("instances", []RdsInstance{}).Label("实例列表"),
		field.Int("count").Label("个数"),
	}
}

// RdsInstance 一个实例的概要。
type RdsInstance struct {
	ID          string `sokel:"id" label:"实例 ID"`
	Description string `sokel:"description,optional" label:"备注名"`
	Engine      string `sokel:"engine" label:"引擎"`
	Version     string `sokel:"version,optional" label:"引擎版本"`
	Status      string `sokel:"status" label:"状态"`
	Class       string `sokel:"class,optional" label:"规格"`
	ExpireTime  string `sokel:"expire_time,optional" label:"到期时间"`
}

// RdsInstanceDetail 实例详情（含容量水位）。
type RdsInstanceDetail struct{}

func (RdsInstanceDetail) Meta() contract.Meta {
	return contract.Meta{ID: "rds_instance_detail", Label: "RDS·实例详情",
		Desc: "单个实例的详情：磁盘容量与已用、最大连接数、内存、维护窗口"}
}

func (RdsInstanceDetail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("instance_id").Label("实例 ID").Desc("rm- 开头"),
	}
}

func (RdsInstanceDetail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("status").Label("状态"),
		field.Int("disk_gb").Label("磁盘容量(GB)"),
		field.Number("disk_used_gb").Label("磁盘已用(GB)").Desc("来自实例资源用量接口；≈0 时以云监控指标为准"),
		field.Int("max_connections").Label("最大连接数"),
		field.Int("memory_mb").Label("内存(MB)"),
		field.String("maintain_time").Label("维护窗口"),
		field.Any("raw", "DescribeDBInstanceAttribute 的原始应答项，形状由阿里云定义").Label("原始详情"),
	}
}

// RdsSlowLogs 慢 SQL 汇总。
type RdsSlowLogs struct{}

func (RdsSlowLogs) Meta() contract.Meta {
	return contract.Meta{ID: "rds_slow_logs", Label: "RDS·慢SQL",
		Desc: "一段时间内的慢 SQL 汇总（按模板聚合：执行次数/平均耗时/扫描行数）"}
}

func (RdsSlowLogs) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("instance_id").Label("实例 ID"),
		field.Int("days").Label("查最近几天").Desc("默认 1；阿里云按天聚合，时间只能到天粒度").Optional(),
	}
}

func (RdsSlowLogs) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("slow_sqls", []RdsSlowSQL{}).Label("慢SQL列表"),
		field.Int("count").Label("条数"),
	}
}

// RdsSlowSQL 一条慢 SQL 模板。
type RdsSlowSQL struct {
	SQLText          string  `sokel:"sql_text" label:"SQL 模板"`
	Database         string  `sokel:"database,optional" label:"库"`
	ExecuteTimes     int     `sokel:"execute_times" label:"执行次数"`
	AvgSeconds       float64 `sokel:"avg_seconds" label:"平均耗时(秒)"`
	ParseRowCounts   int64   `sokel:"parse_rows,optional" label:"平均扫描行数"`
	ReturnRowCounts  int64   `sokel:"return_rows,optional" label:"平均返回行数"`
	CreateTimeReport string  `sokel:"report_date,optional" label:"统计日期"`
}

// —— DNS ——

// DnsRecords 解析记录列表。
type DnsRecords struct{}

func (DnsRecords) Meta() contract.Meta {
	return contract.Meta{ID: "dns_records", Label: "DNS·解析记录",
		Desc: "列出域名的解析记录（可按主机记录/类型过滤）"}
}

func (DnsRecords) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("domain").Label("域名").Desc("如 example.com（不带主机记录）"),
		field.String("rr").Label("主机记录过滤").Desc("如 www、@；留空取全部").Optional(),
		field.String("type").Label("类型过滤").Desc("A/CNAME/TXT/MX…；留空取全部").Optional(),
	}
}

func (DnsRecords) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("records", []DnsRecord{}).Label("记录列表"),
		field.Int("count").Label("条数"),
	}
}

// DnsRecord 一条解析记录。
type DnsRecord struct {
	RecordID string `sokel:"record_id" label:"记录 ID"`
	RR       string `sokel:"rr" label:"主机记录"`
	Type     string `sokel:"type" label:"类型"`
	Value    string `sokel:"value" label:"记录值"`
	TTL      int    `sokel:"ttl" label:"TTL"`
	Status   string `sokel:"status" label:"状态" desc:"ENABLE/DISABLE"`
}

// DnsAddRecord 加解析记录。
type DnsAddRecord struct{}

func (DnsAddRecord) Meta() contract.Meta {
	return contract.Meta{ID: "dns_add_record", Label: "DNS·加记录",
		Desc: "添加一条解析记录（证书 DNS-01 验证、上新服务）。同名同类型已存在时阿里云会照加不去重——先查再加"}
}

func (DnsAddRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("domain").Label("域名"),
		field.String("rr").Label("主机记录").Desc("www / @ / _acme-challenge 等"),
		field.Enum("type",
			field.Opt("A", "A"), field.Opt("AAAA", "AAAA"), field.Opt("CNAME", "CNAME"),
			field.Opt("TXT", "TXT"), field.Opt("MX", "MX"), field.Opt("SRV", "SRV")).
			Label("类型").Default("A"),
		field.String("value").Label("记录值"),
		field.Int("ttl").Label("TTL").Desc("默认 600；免费版最低 600").Optional(),
	}
}

func (DnsAddRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("记录 ID")}
}

// DnsUpdateRecord 改解析记录。
type DnsUpdateRecord struct{}

func (DnsUpdateRecord) Meta() contract.Meta {
	return contract.Meta{ID: "dns_update_record", Label: "DNS·改记录",
		Desc: "按 record_id 改一条记录（切流量就是改 A/CNAME 的值）"}
}

func (DnsUpdateRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("record_id").Label("记录 ID").Desc("来自「解析记录」的产出"),
		field.String("rr").Label("主机记录"),
		field.String("type").Label("类型").Desc("A/CNAME/TXT…"),
		field.String("value").Label("记录值"),
		field.Int("ttl").Label("TTL").Optional(),
	}
}

func (DnsUpdateRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// DnsDeleteRecord 删解析记录。
type DnsDeleteRecord struct{}

func (DnsDeleteRecord) Meta() contract.Meta {
	return contract.Meta{ID: "dns_delete_record", Label: "DNS·删记录",
		Desc: "按 record_id 删一条记录（清理 DNS-01 验证记录）"}
}

func (DnsDeleteRecord) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.String("record_id").Label("记录 ID")}
}

func (DnsDeleteRecord) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{field.Bool("ok").Label("成功")}
}

// —— ACK ——

// AckClusters 集群列表。
type AckClusters struct{}

func (AckClusters) Meta() contract.Meta {
	return contract.Meta{ID: "ack_clusters", Label: "ACK·集群列表",
		Desc: "列出容器服务集群（状态/版本/规模）"}
}

func (AckClusters) Inputs() []contract.FieldSpec { return nil }

func (AckClusters) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("clusters", []AckCluster{}).Label("集群列表"),
		field.Int("count").Label("个数"),
	}
}

// AckCluster 一个集群概要。
type AckCluster struct {
	ClusterID string `sokel:"cluster_id" label:"集群 ID"`
	Name      string `sokel:"name" label:"名称"`
	State     string `sokel:"state" label:"状态"`
	Version   string `sokel:"version,optional" label:"K8s 版本"`
	Region    string `sokel:"region,optional" label:"Region"`
	Size      int    `sokel:"size,optional" label:"节点数"`
	Type      string `sokel:"type,optional" label:"类型" desc:"ManagedKubernetes 等"`
}

// AckKubeconfig 导出 kubeconfig。
type AckKubeconfig struct{}

func (AckKubeconfig) Meta() contract.Meta {
	return contract.Meta{ID: "ack_kubeconfig", Label: "ACK·导出 kubeconfig",
		Desc: "取集群的 kubeconfig（私网或公网端点），粘进 kubernetes 插件的凭证即可在画布上操作集群内工作负载。" +
			"**产出是能进集群的钥匙**，别把它接到会外发/落库的节点上"}
}

func (AckKubeconfig) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("cluster_id").Label("集群 ID"),
		field.Bool("private").Label("私网端点").Desc("插件与集群同 VPC 时开；默认公网端点").Default(false),
	}
}

func (AckKubeconfig) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("kubeconfig").Label("kubeconfig").Desc("YAML 全文；有效期由集群配置决定"),
	}
}

// —— 云监控 ——

// CmsMetric 查指标。
type CmsMetric struct{}

func (CmsMetric) Meta() contract.Meta {
	return contract.Meta{ID: "cms_metric", Label: "云监控·查指标",
		Desc: "查一段时间的监控指标（RDS 磁盘水位/CPU、ECS、SLB…）。命名空间与指标名照云监控文档，" +
			"如 acs_rds_dashboard 的 DiskUsage、ConnectionUsage"}
}

func (CmsMetric) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("namespace").Label("命名空间").Desc("acs_rds_dashboard / acs_ecs_dashboard / acs_kubernetes…"),
		field.String("metric").Label("指标名").Desc("DiskUsage / CpuUsage / ConnectionUsage…"),
		field.String("instance_id").Label("实例 ID").Desc("按实例过滤；留空查全部").Optional(),
		field.Int("minutes").Label("查最近几分钟").Desc("默认 60").Optional(),
		field.String("period").Label("聚合周期(秒)").Desc("默认该指标的最小周期（通常 60）").Optional(),
	}
}

func (CmsMetric) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Array("points", []map[string]any{}).Label("数据点").
			Desc("每点含 timestamp 与 Average/Maximum/Minimum 等，具体键由指标定义——各指标不同，无法预声明"),
		field.Number("latest").Label("最新值").Desc("最后一个点的 Average（无点时为 0）"),
		field.Int("count").Label("点数"),
	}
}

// —— 移动推送（EMAS）——

// Push 推送到 App。
type Push struct{}

func (Push) Meta() contract.Meta {
	return contract.Meta{ID: "push", Label: "移动推送·推送",
		Desc: "EMAS 移动推送：给 App 推通知（弹在通知栏）或消息（透传给应用代码）。" +
			"按设备/账号/别名/标签圈人，或全量广播。需要 push 系列 RAM 权限"}
}

func (Push) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("app_key").Label("AppKey").
			Desc("EMAS 控制台该 App 的数字 AppKey（不是凭证——它定位推给哪个 App）"),
		field.Enum("target",
			field.Opt("ALL", "全量广播"),
			field.Opt("DEVICE", "按设备 ID"),
			field.Opt("ACCOUNT", "按账号"),
			field.Opt("ALIAS", "按别名"),
			field.Opt("TAG", "按标签")).
			Label("目标类型").Default("ALL"),
		field.String("target_value").Label("目标值").
			Desc("设备/账号/别名/标签，多个逗号分隔（上限 1000）；全量广播填 ALL 或留空").Optional(),
		field.Enum("device_type",
			field.Opt("ALL", "Android + iOS"),
			field.Opt("ANDROID", "仅 Android"),
			field.Opt("iOS", "仅 iOS")).
			Label("设备类型").Default("ALL"),
		field.Enum("push_type",
			field.Opt("NOTICE", "通知（弹通知栏）"),
			field.Opt("MESSAGE", "消息（透传给应用代码，不弹）")).
			Label("推送类型").Default("NOTICE"),
		field.String("title").Label("标题"),
		field.Text("body").Label("内容"),
		field.Enum("ios_env",
			field.Opt("PRODUCT", "生产环境"),
			field.Opt("DEV", "开发环境")).
			Label("iOS 推送环境").Desc("走 APNs 时区分证书环境；只推 Android 可忽略").Default("PRODUCT").Optional(),
		field.Object("extras", "自定义键值对，透传给客户端（Android/iOS 各自的扩展参数一起下发），形状由你的 App 约定").
			Label("附加参数").Optional(),
	}
}

func (Push) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("message_id").Label("消息 ID").Desc("查送达统计（call 调 QueryPushStatByMsg）认它"),
	}
}

// —— 邮件推送（DirectMail）——

// SendMail 发邮件。
type SendMail struct{}

func (SendMail) Meta() contract.Meta {
	return contract.Meta{ID: "send_mail", Label: "邮件推送·发邮件",
		Desc: "阿里云邮件推送（DirectMail）单发：HTML 或纯文本。发信地址要先在控制台配好域名并验证。" +
			"需要 dm 系列 RAM 权限"}
}

func (SendMail) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("account_name").Label("发信地址").
			Desc("DirectMail 控制台配好的发信地址（如 notice@mail.yourdomain.com）"),
		field.String("from_alias").Label("发信人昵称").Desc("收件箱里显示的名字").Optional(),
		field.String("to").Label("收件人").Desc("多个逗号分隔（单次上限 100）"),
		field.String("subject").Label("主题"),
		field.Text("html").Label("HTML 正文").Desc("与纯文本二选一；都填时用 HTML").Optional(),
		field.Text("text").Label("纯文本正文").Optional(),
		field.String("reply_to").Label("回信地址").Desc("收件人点「回复」时回到这里").Optional(),
	}
}

func (SendMail) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("env_id").Label("发送回执 ID"),
	}
}

// —— 保底 / 体检 ——

// Call 泛化调用：任意 RPC 产品。
type Call struct{}

func (Call) Meta() contract.Meta {
	return contract.Meta{ID: "call", Label: "通用调用", TimeoutSec: 60,
		Desc: "直调任意阿里云 RPC 风格 OpenAPI（统一签名网关）：给 endpoint + action + version + 参数。" +
			"typed 操作没覆盖的产品与高危操作（重启实例等）走这里——参数照各产品的 API 文档"}
}

func (Call) Inputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.String("endpoint").Label("Endpoint").
			Desc("如 rds.aliyuncs.com、ecs.cn-hangzhou.aliyuncs.com（照产品 API 文档的服务接入点）"),
		field.String("action").Label("Action").Desc("如 DescribeDBInstances"),
		field.String("version").Label("Version").Desc("该产品的 API 版本日期，如 2014-08-15"),
		field.Object("params", "该 Action 的参数对象，键名照阿里云 API 文档；本插件不介入").Label("参数").Optional(),
	}
}

func (Call) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Any("data", "阿里云应答 body，形状随 Action 而变，由各产品 API 文档定义").Label("应答"),
	}
}

// HealthCheck 平台约定的凭证体检。
type HealthCheck struct{}

func (HealthCheck) Meta() contract.Meta {
	return contract.Meta{ID: "health_check", Label: "检查凭证", TimeoutSec: 30,
		Desc: "调 STS GetCallerIdentity 验证 AccessKey 有效，并显示这把钥匙是谁（RAM 用户/主账号）"}
}

func (HealthCheck) Inputs() []contract.FieldSpec { return nil }

func (HealthCheck) Outputs() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Bool("ok").Label("可用"),
		field.String("identity").Label("身份").Desc("ARN，例如 acs:ram::123:user/ops-readonly"),
		field.String("message").Label("说明"),
	}
}

// —— 凭证 ——

// Credential：AccessKey。建议用最小权限的 RAM 用户（策略样例见使用说明），别用主账号 AK。
type Credential struct{}

func (Credential) CredentialFields() []contract.FieldSpec {
	return []contract.FieldSpec{
		field.Text("access_key_id").Label("AccessKey ID").
			Desc("RAM 控制台创建的 AccessKey（建议专建一个最小权限的 RAM 用户，策略样例见使用说明）"),
		field.Secret("access_key_secret").Label("AccessKey Secret").
			Desc("只在插件内部签名，不进节点与日志"),
		field.Text("region").Label("默认 Region").
			Desc("如 cn-hangzhou；SLS/云监控/RDS 列表默认查这个 region").Optional(),
	}
}
