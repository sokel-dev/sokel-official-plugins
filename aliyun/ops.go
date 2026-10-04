package main

// All operation implementations. For each product we've only made two or three high-frequency
// endpoints typed; the rest go through call. Each endpoint's Action/Version constants are
// gathered here so they're easy to spot when the API version changes.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	openapi "github.com/alibabacloud-go/darabonba-openapi/v2/client"
	"github.com/alibabacloud-go/tea/dara"
	"github.com/alibabacloud-go/tea/tea"

	"github.com/sokel-dev/sokel-official-plugins/aliyun/schema"
	"github.com/sokel-dev/sokel-plugin-sdk/plugin"
)

// parseJSONArray — CloudMonitor stuffs Datapoints into a JSON string (legacy baggage); if it
// can't be parsed, treat it as empty.
func parseJSONArray(s string) []map[string]any {
	var out []map[string]any
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

// —— SLS ——

// parseWhen converts RFC3339 or a second-level timestamp into Unix seconds.
func parseWhen(s string, def int64) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return def, nil
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix(), nil
	}
	return 0, fmt.Errorf("时间 %q 认不出来——用 RFC3339（2026-08-20T10:00:00+08:00）或秒级时间戳", s)
}

func opSlsQuery(ctx plugin.Ctx, in *SlsQueryIn) (*SlsQueryOut, error) {
	project, logstore := strings.TrimSpace(in.Project), strings.TrimSpace(in.Logstore)
	if project == "" || logstore == "" {
		return nil, fmt.Errorf("project 与 logstore 都要填")
	}
	now := time.Now().Unix()
	minutes := in.Minutes
	if minutes <= 0 {
		minutes = 15
	}
	from, err := parseWhen(in.From, now-int64(minutes)*60)
	if err != nil {
		return nil, err
	}
	to, err := parseWhen(in.To, now)
	if err != nil {
		return nil, err
	}
	if from >= to {
		return nil, fmt.Errorf("开始时间要早于结束时间（from=%d to=%d）", from, to)
	}
	limit := int64(in.Limit)
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	c, err := slsClientOf(credOf(ctx), regionOf(credOf(ctx), ""))
	if err != nil {
		return nil, err
	}
	resp, err := c.GetLogs(project, logstore, "", from, to, in.Query, limit, 0, true)
	if err != nil {
		return nil, slsErr(err)
	}
	logs := make([]map[string]any, 0, len(resp.Logs))
	for _, l := range resp.Logs {
		row := make(map[string]any, len(l))
		for k, v := range l {
			row[k] = v
		}
		logs = append(logs, row)
	}
	return &SlsQueryOut{Logs: anySlice(logs), Count: len(logs), Complete: resp.IsComplete()}, nil
}

func anySlice[T any](in []T) []any {
	out := make([]any, len(in))
	for i, v := range in {
		out[i] = v
	}
	return out
}

func opSlsListLogstores(ctx plugin.Ctx, in *SlsListLogstoresIn) (*SlsListLogstoresOut, error) {
	project := strings.TrimSpace(in.Project)
	if project == "" {
		return nil, fmt.Errorf("project 是空的")
	}
	c, err := slsClientOf(credOf(ctx), regionOf(credOf(ctx), ""))
	if err != nil {
		return nil, err
	}
	names, err := c.ListLogStore(project)
	if err != nil {
		return nil, slsErr(err)
	}
	return &SlsListLogstoresOut{Logstores: names}, nil
}

// —— RDS ——

const (
	rdsEndpoint = "rds.aliyuncs.com"
	rdsVersion  = "2014-08-15"
)

func opRdsInstances(ctx plugin.Ctx, in *RdsInstancesIn) (*RdsInstancesOut, error) {
	cred := credOf(ctx)
	body, err := callACS(ctx, cred, rdsEndpoint, "DescribeDBInstances", rdsVersion, map[string]any{
		"RegionId": regionOf(cred, in.Region), "PageSize": 100,
	})
	if err != nil {
		return nil, err
	}
	items := digList(body, "Items", "DBInstance")
	out := &RdsInstancesOut{}
	for _, it := range items {
		out.Instances = append(out.Instances, schema.RdsInstance{
			ID:          digStr(it, "DBInstanceId"),
			Description: digStr(it, "DBInstanceDescription"),
			Engine:      digStr(it, "Engine"),
			Version:     digStr(it, "EngineVersion"),
			Status:      digStr(it, "DBInstanceStatus"),
			Class:       digStr(it, "DBInstanceClass"),
			ExpireTime:  digStr(it, "ExpireTime"),
		})
	}
	out.Count = len(out.Instances)
	return out, nil
}

func opRdsInstanceDetail(ctx plugin.Ctx, in *RdsInstanceDetailIn) (*RdsInstanceDetailOut, error) {
	id := strings.TrimSpace(in.InstanceID)
	if id == "" {
		return nil, fmt.Errorf("实例 ID 是空的（rm- 开头，来自实例列表）")
	}
	cred := credOf(ctx)
	body, err := callACS(ctx, cred, rdsEndpoint, "DescribeDBInstanceAttribute", rdsVersion, map[string]any{
		"DBInstanceId": id,
	})
	if err != nil {
		return nil, err
	}
	items := digList(body, "Items", "DBInstanceAttribute")
	if len(items) == 0 {
		return nil, fmt.Errorf("没查到实例 %s——核对实例 ID 与 region", id)
	}
	it := items[0]
	out := &RdsInstanceDetailOut{
		Status:         digStr(it, "DBInstanceStatus"),
		DiskGb:         digInt(it, "DBInstanceStorage"),
		MaxConnections: digInt(it, "MaxConnections"),
		MemoryMb:       digInt(it, "DBInstanceMemory"),
		MaintainTime:   digStr(it, "MaintainTime"),
		Raw:            it,
	}
	// Disk usage only comes from a different endpoint; if that lookup fails, don't fail
	// the whole call — still return the rest of the detail.
	if res, rerr := callACS(ctx, cred, rdsEndpoint, "DescribeDBInstanceResourceUsage", rdsVersion,
		map[string]any{"DBInstanceId": id}); rerr == nil {
		out.DiskUsedGb = digFloat(res, "DiskUsed") / (1 << 30)
	}
	return out, nil
}

func opRdsSlowLogs(ctx plugin.Ctx, in *RdsSlowLogsIn) (*RdsSlowLogsOut, error) {
	id := strings.TrimSpace(in.InstanceID)
	if id == "" {
		return nil, fmt.Errorf("实例 ID 是空的")
	}
	days := in.Days
	if days <= 0 {
		days = 1
	}
	// Alibaba Cloud requires UTC yyyy-MM-ddZ, and end excludes the current day — so we take
	// [today-days, tomorrow).
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -days).Format("2006-01-02") + "Z"
	end := now.AddDate(0, 0, 1).Format("2006-01-02") + "Z"
	body, err := callACS(ctx, credOf(ctx), rdsEndpoint, "DescribeSlowLogs", rdsVersion, map[string]any{
		"DBInstanceId": id, "StartTime": start, "EndTime": end, "PageSize": 100,
	})
	if err != nil {
		return nil, err
	}
	items := digList(body, "Items", "SQLSlowLog")
	out := &RdsSlowLogsOut{}
	for _, it := range items {
		times := digInt(it, "MySQLTotalExecutionCounts")
		totalSec := digFloat(it, "MySQLTotalExecutionTimes")
		avg := 0.0
		if times > 0 {
			avg = totalSec / float64(times)
		}
		out.SlowSqls = append(out.SlowSqls, schema.RdsSlowSQL{
			SQLText:          digStr(it, "SQLText"),
			Database:         digStr(it, "DBName"),
			ExecuteTimes:     times,
			AvgSeconds:       avg,
			ParseRowCounts:   int64(digInt(it, "ParseMaxRowCount")),
			ReturnRowCounts:  int64(digInt(it, "ReturnMaxRowCount")),
			CreateTimeReport: digStr(it, "CreateTime"),
		})
	}
	out.Count = len(out.SlowSqls)
	return out, nil
}

// —— DNS ——

const (
	dnsEndpoint = "alidns.aliyuncs.com"
	dnsVersion  = "2015-01-09"
)

func opDnsRecords(ctx plugin.Ctx, in *DNSRecordsIn) (*DNSRecordsOut, error) {
	domain := strings.TrimSpace(in.Domain)
	if domain == "" {
		return nil, fmt.Errorf("域名是空的（如 example.com，不带主机记录）")
	}
	params := map[string]any{"DomainName": domain, "PageSize": 500}
	if rr := strings.TrimSpace(in.Rr); rr != "" {
		params["RRKeyWord"] = rr
	}
	if t := strings.TrimSpace(in.Type); t != "" {
		params["Type"] = strings.ToUpper(t)
	}
	body, err := callACS(ctx, credOf(ctx), dnsEndpoint, "DescribeDomainRecords", dnsVersion, params)
	if err != nil {
		return nil, err
	}
	items := digList(body, "DomainRecords", "Record")
	out := &DNSRecordsOut{}
	for _, it := range items {
		out.Records = append(out.Records, schema.DnsRecord{
			RecordID: digStr(it, "RecordId"),
			RR:       digStr(it, "RR"),
			Type:     digStr(it, "Type"),
			Value:    digStr(it, "Value"),
			TTL:      digInt(it, "TTL"),
			Status:   digStr(it, "Status"),
		})
	}
	out.Count = len(out.Records)
	return out, nil
}

func opDnsAddRecord(ctx plugin.Ctx, in *DNSAddRecordIn) (*DNSAddRecordOut, error) {
	domain, rr, value := strings.TrimSpace(in.Domain), strings.TrimSpace(in.Rr), strings.TrimSpace(in.Value)
	if domain == "" || rr == "" || value == "" {
		return nil, fmt.Errorf("域名 / 主机记录 / 记录值都要填")
	}
	params := map[string]any{"DomainName": domain, "RR": rr, "Type": strings.ToUpper(in.Type), "Value": value}
	if in.TTL > 0 {
		params["TTL"] = in.TTL
	}
	body, err := callACS(ctx, credOf(ctx), dnsEndpoint, "AddDomainRecord", dnsVersion, params)
	if err != nil {
		return nil, err
	}
	return &DNSAddRecordOut{RecordID: digStr(body, "RecordId")}, nil
}

func opDnsUpdateRecord(ctx plugin.Ctx, in *DNSUpdateRecordIn) (*DNSUpdateRecordOut, error) {
	rid := strings.TrimSpace(in.RecordID)
	if rid == "" {
		return nil, fmt.Errorf("记录 ID 是空的（来自「解析记录」的产出）")
	}
	params := map[string]any{"RecordId": rid, "RR": strings.TrimSpace(in.Rr),
		"Type": strings.ToUpper(strings.TrimSpace(in.Type)), "Value": strings.TrimSpace(in.Value)}
	if in.TTL > 0 {
		params["TTL"] = in.TTL
	}
	if _, err := callACS(ctx, credOf(ctx), dnsEndpoint, "UpdateDomainRecord", dnsVersion, params); err != nil {
		// When the value is unchanged, Alibaba Cloud returns DomainRecordDuplicate — to the
		// caller this counts as an idempotent success.
		if strings.Contains(err.Error(), "DomainRecordDuplicate") {
			return &DNSUpdateRecordOut{OK: true}, nil
		}
		return nil, err
	}
	return &DNSUpdateRecordOut{OK: true}, nil
}

func opDnsDeleteRecord(ctx plugin.Ctx, in *DNSDeleteRecordIn) (*DNSDeleteRecordOut, error) {
	rid := strings.TrimSpace(in.RecordID)
	if rid == "" {
		return nil, fmt.Errorf("记录 ID 是空的")
	}
	if _, err := callACS(ctx, credOf(ctx), dnsEndpoint, "DeleteDomainRecord", dnsVersion,
		map[string]any{"RecordId": rid}); err != nil {
		return nil, err
	}
	return &DNSDeleteRecordOut{OK: true}, nil
}

// —— ACK ——
//
// Container Service is ROA-style (RESTful paths); the generic call switches to Style=ROA +
// Pathname for it.

const csVersion = "2015-12-15"

func csEndpoint(cred Cred, region string) string {
	return "cs." + regionOf(cred, region) + ".aliyuncs.com"
}

func callCS(ctx plugin.Ctx, cred Cred, method, pathname string, query map[string]any) (map[string]any, error) {
	c, err := acsClientOf(cred, csEndpoint(cred, ""))
	if err != nil {
		return nil, err
	}
	q := map[string]*string{}
	for k, v := range query {
		q[k] = tea.String(fmt.Sprintf("%v", v))
	}
	res, err := c.CallApi(&openapi.Params{
		Action: tea.String(pathname), Version: tea.String(csVersion),
		Protocol: tea.String("HTTPS"), Method: tea.String(method), AuthType: tea.String("AK"),
		Style: tea.String("ROA"), Pathname: tea.String(pathname),
		ReqBodyType: tea.String("json"), BodyType: tea.String("json"),
	}, &openapi.OpenApiRequest{Query: q}, &dara.RuntimeOptions{})
	if err != nil {
		return nil, acsErr("CS "+pathname, err)
	}
	body, _ := res["body"].(map[string]any)
	if body == nil {
		body = res
	}
	return body, nil
}

func opAckClusters(ctx plugin.Ctx, _ *AckClustersIn) (*AckClustersOut, error) {
	body, err := callCS(ctx, credOf(ctx), "GET", "/api/v1/clusters", nil)
	if err != nil {
		return nil, err
	}
	items := digList(body, "clusters")
	if len(items) == 0 { // tolerate a flat response shape
		items = digList(map[string]any{"x": body["clusters"]}, "x")
	}
	out := &AckClustersOut{}
	for _, it := range items {
		out.Clusters = append(out.Clusters, schema.AckCluster{
			ClusterID: digStr(it, "cluster_id"),
			Name:      digStr(it, "name"),
			State:     digStr(it, "state"),
			Version:   digStr(it, "current_version"),
			Region:    digStr(it, "region_id"),
			Size:      digInt(it, "size"),
			Type:      digStr(it, "cluster_type"),
		})
	}
	out.Count = len(out.Clusters)
	return out, nil
}

func opAckKubeconfig(ctx plugin.Ctx, in *AckKubeconfigIn) (*AckKubeconfigOut, error) {
	cid := strings.TrimSpace(in.ClusterID)
	if cid == "" {
		return nil, fmt.Errorf("集群 ID 是空的（来自「集群列表」）")
	}
	q := map[string]any{}
	if in.Private {
		q["PrivateIpAddress"] = "true"
	}
	body, err := callCS(ctx, credOf(ctx), "GET", "/k8s/"+cid+"/user_config", q)
	if err != nil {
		return nil, err
	}
	kc := digStr(body, "config")
	if kc == "" {
		return nil, fmt.Errorf("没拿到 kubeconfig——集群状态非 running，或 RAM 用户缺 cs:DescribeClusterUserKubeconfig 权限")
	}
	return &AckKubeconfigOut{Kubeconfig: kc}, nil
}

// —— CloudMonitor ——

const cmsVersion = "2019-01-01"

func opCmsMetric(ctx plugin.Ctx, in *CmsMetricIn) (*CmsMetricOut, error) {
	ns, metric := strings.TrimSpace(in.Namespace), strings.TrimSpace(in.Metric)
	if ns == "" || metric == "" {
		return nil, fmt.Errorf("命名空间与指标名都要填（照云监控文档，如 acs_rds_dashboard / DiskUsage）")
	}
	cred := credOf(ctx)
	minutes := in.Minutes
	if minutes <= 0 {
		minutes = 60
	}
	now := time.Now()
	params := map[string]any{
		"Namespace": ns, "MetricName": metric,
		"StartTime": now.Add(-time.Duration(minutes) * time.Minute).UnixMilli(),
		"EndTime":   now.UnixMilli(),
		"Length":    1000,
	}
	if p := strings.TrimSpace(in.Period); p != "" {
		params["Period"] = p
	}
	if id := strings.TrimSpace(in.InstanceID); id != "" {
		params["Dimensions"] = `[{"instanceId":"` + id + `"}]`
	}
	body, err := callACS(ctx, cred, "metrics."+regionOf(cred, "")+".aliyuncs.com",
		"DescribeMetricList", cmsVersion, params)
	if err != nil {
		return nil, err
	}
	// Datapoints is a **JSON string**, not an array (CloudMonitor legacy baggage).
	var points []map[string]any
	if s := digStr(body, "Datapoints"); s != "" && s != "[]" {
		points = parseJSONArray(s)
	}
	out := &CmsMetricOut{Points: anySlice(points), Count: len(points)}
	if n := len(points); n > 0 {
		out.Latest = digFloat(points[n-1], "Average")
	}
	return out, nil
}

// —— Mobile push (EMAS) ——

const (
	pushEndpoint = "cloudpush.aliyuncs.com"
	pushVersion  = "2016-08-01"
)

func opPush(ctx plugin.Ctx, in *PushIn) (*PushOut, error) {
	appKey := strings.TrimSpace(in.AppKey)
	if appKey == "" {
		return nil, fmt.Errorf("AppKey 是空的——EMAS 控制台该 App 的数字 AppKey")
	}
	title, body := strings.TrimSpace(in.Title), strings.TrimSpace(in.Body)
	if title == "" || body == "" {
		return nil, fmt.Errorf("标题与内容都要填")
	}
	target := strings.ToUpper(strings.TrimSpace(in.Target))
	if target == "" {
		target = "ALL"
	}
	targetValue := strings.TrimSpace(in.TargetValue)
	if target == "ALL" {
		targetValue = "ALL" // when broadcasting, Alibaba Cloud requires TargetValue to also be ALL
	} else if targetValue == "" {
		return nil, fmt.Errorf("目标类型是 %s 时必须填目标值（多个逗号分隔）", target)
	}
	deviceType := strings.TrimSpace(in.DeviceType)
	if deviceType == "" {
		deviceType = "ALL"
	}
	pushType := strings.ToUpper(strings.TrimSpace(in.PushType))
	if pushType == "" {
		pushType = "NOTICE"
	}
	params := map[string]any{
		"AppKey": appKey, "PushType": pushType, "DeviceType": deviceType,
		"Target": target, "TargetValue": targetValue,
		"Title": title, "Body": body,
	}
	// iOS over APNs must declare the certificate environment; this param is harmless when
	// only pushing to Android.
	env := strings.TrimSpace(in.IosEnv)
	if env == "" {
		env = "PRODUCT"
	}
	if deviceType == "ALL" || deviceType == "iOS" {
		params["iOSApnsEnv"] = env
	}
	if len(in.Extras) > 0 {
		// Custom params are two separate inputs for Android/iOS; the same extras get sent to
		// both — each client picks what it needs.
		b, _ := json.Marshal(in.Extras)
		params["AndroidExtParameters"] = string(b)
		params["iOSExtParameters"] = string(b)
	}
	body2, err := callACS(ctx, credOf(ctx), pushEndpoint, "Push", pushVersion, params)
	if err != nil {
		return nil, err
	}
	return &PushOut{MessageID: digStr(body2, "MessageId")}, nil
}

// —— Email push (DirectMail) ——

func opSendMail(ctx plugin.Ctx, in *SendMailIn) (*SendMailOut, error) {
	account, to, subject := strings.TrimSpace(in.AccountName), strings.TrimSpace(in.To), strings.TrimSpace(in.Subject)
	if account == "" {
		return nil, fmt.Errorf("发信地址是空的——DirectMail 控制台「发信地址」里配好的那个")
	}
	if to == "" || subject == "" {
		return nil, fmt.Errorf("收件人与主题都要填")
	}
	html, text := strings.TrimSpace(in.HTML), strings.TrimSpace(in.Text)
	if html == "" && text == "" {
		return nil, fmt.Errorf("HTML 与纯文本正文至少填一个")
	}
	params := map[string]any{
		"AccountName": account, "AddressType": 1, "ReplyToAddress": false,
		"ToAddress": to, "Subject": subject,
	}
	if html != "" {
		params["HtmlBody"] = html
	} else {
		params["TextBody"] = text
	}
	if fa := strings.TrimSpace(in.FromAlias); fa != "" {
		params["FromAlias"] = fa
	}
	if rt := strings.TrimSpace(in.ReplyTo); rt != "" {
		params["ReplyAddress"] = rt
		params["ReplyAddressAlias"] = rt
	}
	body, err := callACS(ctx, credOf(ctx), "dm.aliyuncs.com", "SingleSendMail", "2015-11-23", params)
	if err != nil {
		return nil, err
	}
	return &SendMailOut{EnvID: digStr(body, "EnvId")}, nil
}

// —— Fallback / health check ——

func opCall(ctx plugin.Ctx, in *CallIn) (*CallOut, error) {
	ep, action, version := strings.TrimSpace(in.Endpoint), strings.TrimSpace(in.Action), strings.TrimSpace(in.Version)
	if ep == "" || action == "" || version == "" {
		return nil, fmt.Errorf("endpoint / action / version 都要填（照产品 API 文档）")
	}
	if strings.Contains(ep, "/") {
		return nil, fmt.Errorf("endpoint 只填域名（如 rds.aliyuncs.com），不带路径")
	}
	body, err := callACS(ctx, credOf(ctx), ep, action, version, in.Params)
	if err != nil {
		return nil, err
	}
	return &CallOut{Data: body}, nil
}

func opHealthCheck(ctx plugin.Ctx, _ *HealthCheckIn) (*HealthCheckOut, error) {
	body, err := callACS(ctx, credOf(ctx), "sts.aliyuncs.com", "GetCallerIdentity", "2015-04-01", nil)
	if err != nil {
		return &HealthCheckOut{OK: false, Message: err.Error()}, nil
	}
	arn := digStr(body, "Arn")
	return &HealthCheckOut{OK: true, Identity: arn, Message: "AccessKey 可用：" + arn}, nil
}
