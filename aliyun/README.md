# aliyun — 阿里云管控面插件（第一方自部署）

15 个操作：SLS 查日志、RDS（实例/详情/慢SQL）、DNS（查/加/改/删）、ACK（集群/kubeconfig）、
云监控查指标、EMAS 移动推送 + `call` 泛化保底 + `health_check`（STS GetCallerIdentity）。
面向用户的说明书是 [docs/aliyun.md](docs/aliyun.md)。
集群内工作负载操作在 `../kubernetes`（本插件 ack_kubeconfig 的产出喂它）。

## 架构判断

- **泛化调用是本插件的地基**：阿里云 OpenAPI 是统一签名网关，
  `darabonba-openapi/v2` 一个包用 {Endpoint, Action, Version} 调任意 RPC 产品。
  不引 per-product 生成 SDK（几十个巨包 vs 我们每家用两三个接口）。
  typed 操作内部走同一条 `callACS`，只是把参数拼好。
- **SLS 是唯一例外**：独立协议独立签名，走官方 aliyun-log-go-sdk。
- **ACK（容器服务）是 ROA 风格**（RESTful 路径），泛化调用换 `Style: ROA + Pathname`
  （`callCS`），与 RPC 的 `callACS` 分开。
- **高危写不做 typed**：RDS 重启/删实例只能走 call 显式拼——权限控制点在 RAM
  （docs 给了最小权限策略），插件不自造权限系统。

## 坑（改代码前先读）

- **阿里云列表都包双层壳**：`{"Items":{"DBInstance":[...]}}`——`digList` 的路径要写到最里层。
- **数字常以字符串给**：`digInt/digFloat` 都做了字符串兜底。
- **云监控 Datapoints 是 JSON 串不是数组**（历史包袱），要再解一层（`parseJSONArray`）。
- **RDS 慢 SQL 时间只能到天**：UTC `yyyy-MM-ddZ` 格式，end 不含当天。
- **DNS UpdateDomainRecord 值没变回 DomainRecordDuplicate**——那是幂等成功，别当失败。
- **移动推送的 AppKey 是入参不是凭证**（定位推给哪个 App，像 RDS 的实例 ID）；
  广播时 TargetValue 必须也是 "ALL"；iOS 在目标设备类型里就要带 iOSApnsEnv。
- **SLS project 是 region 级资源**：region 错了报 ProjectNotExist，错误翻译里点了这一句。
- 泛化调用强制 HTTPS，httptest 假网关打不穿签名链路——网络面靠 operation:test 真机验，
  测试钉的是解析/翻译/入参卫生这些纯逻辑。

## 开发

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
