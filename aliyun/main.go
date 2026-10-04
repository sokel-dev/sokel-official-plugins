// aliyun —— Sokel 第一方插件：阿里云管控面（SLS/RDS/DNS/ACK/云监控 + call 保底）。
//
// 集群内工作负载操作在通用 kubernetes 插件里（凭证 = kubeconfig，
// 本插件的 ack_kubeconfig 能导出来喂它）。设计判断见 schema/schema.go 顶注。
//
// 运行：SOKEL_ENDPOINT=nats://<broker>:4222 SOKEL_TOKEN=skp_xxx ./aliyun
package main

//go:generate go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen

import (
	"log"
	"os"

	"github.com/sokel-dev/sokel-plugin-sdk/sokel"
)

func main() {
	token := sokel.Env("TOKEN")
	if token == "" && sokel.Env("DEPLOY_KEY") == "" {
		log.Fatal("请设置 SOKEL_TOKEN(接入组「接入命令」里复制);随部署托管的容器可改设 SOKEL_DEPLOY_KEY 自动注册")
	}
	p := sokel.New(sokel.Config{
		Endpoint: env("SOKEL_ENDPOINT", "http://localhost:8088"),
		Token:    token,
		Name:     "aliyun",
	})
	RegisterCredential(p)
	p.SetDoc(usageDoc, "")

	OnSlsQuery(p, opSlsQuery)
	OnSlsListLogstores(p, opSlsListLogstores)
	OnRdsInstances(p, opRdsInstances)
	OnRdsInstanceDetail(p, opRdsInstanceDetail)
	OnRdsSlowLogs(p, opRdsSlowLogs)
	OnDNSRecords(p, opDnsRecords)
	OnDNSAddRecord(p, opDnsAddRecord)
	OnDNSUpdateRecord(p, opDnsUpdateRecord)
	OnDNSDeleteRecord(p, opDnsDeleteRecord)
	OnAckClusters(p, opAckClusters)
	OnAckKubeconfig(p, opAckKubeconfig)
	OnCmsMetric(p, opCmsMetric)
	OnPush(p, opPush)
	OnSendMail(p, opSendMail)
	OnCall(p, opCall)
	OnHealthCheck(p, opHealthCheck)

	if err := p.Run(); err != nil {
		log.Fatal(err)
	}
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
