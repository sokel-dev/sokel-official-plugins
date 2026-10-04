# aliyun — Alibaba Cloud control plane plugin (first-party, self-hosted)

15 operations: SLS log query, RDS (instances/detail/slow SQL), DNS (query/add/update/delete),
ACK (clusters/kubeconfig), CloudMonitor metric query, EMAS mobile push, plus the generic `call`
fallback and `health_check` (STS GetCallerIdentity).
The user-facing manual is [docs/aliyun.md](docs/aliyun.md).
In-cluster workload operations live in `../kubernetes` (fed by this plugin's ack_kubeconfig output).

## Architectural decisions

- **The generic call is this plugin's foundation**: Alibaba Cloud's OpenAPI is a unified signing
  gateway, so a single `darabonba-openapi/v2` package can call any RPC product via
  {Endpoint, Action, Version}. We avoid pulling in per-product generated SDKs (dozens of giant
  packages vs. the two or three endpoints we actually use per vendor). Typed operations internally
  go through the same `callACS`; they just pre-fill the params.
- **SLS is the one exception**: it has its own protocol and signing, via the official
  aliyun-log-go-sdk.
- **ACK (Container Service) is ROA-style** (RESTful paths); the generic call switches to
  `Style: ROA + Pathname` (`callCS`) for it, kept separate from RPC's `callACS`.
- **High-risk writes are not typed**: restarting/deleting an RDS instance can only go through an
  explicitly spelled-out call — the permission control point is RAM (docs include a
  least-privilege policy), and the plugin doesn't invent its own permission system.

## Gotchas (read before touching the code)

- **Alibaba Cloud lists are always double-wrapped**: `{"Items":{"DBInstance":[...]}}` — `digList`'s
  path must point to the innermost level.
- **Numbers often come as strings**: `digInt`/`digFloat` both fall back to parsing strings.
- **CloudMonitor's Datapoints is a JSON string, not an array** (legacy baggage); it needs an extra
  parse (`parseJSONArray`).
- **RDS slow SQL time only goes down to the day**: UTC `yyyy-MM-ddZ` format, end excludes the
  current day.
- **DNS UpdateDomainRecord returns DomainRecordDuplicate when the value is unchanged** — that's an
  idempotent success, not a failure.
- **Mobile push's AppKey is an input, not a credential** (it identifies which App to push to, like
  an RDS instance ID); when broadcasting, TargetValue must also be "ALL"; iOS target device types
  must carry iOSApnsEnv.
- **SLS project is a region-level resource**: the wrong region returns ProjectNotExist, which the
  error translation calls out explicitly.
- The generic call forces HTTPS, and the httptest fake gateway can't exercise the real signing
  path — the network side is verified against the real cloud via operation:test; the tests here
  pin down parsing/translation/input-validation logic only.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
