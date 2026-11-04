# Gitea plugin

Nine typed operations for self-hosted Gitea repository, issue, comment and pull-request workflows. The service root is required in `SOKEL_GITEA_URL`; there is no public fallback. Credentials contain a personal API token, not deployment addresses. See [the user guide](docs/gitea.md) and [中文指南](docs/gitea.zh-CN.md).

The protocol baseline is Gitea **28.3.0**, REST `/api/v1`, MIT licensed. The pinned [official Swagger specification](https://github.com/go-gitea/gitea/blob/v28.3.0/templates/swagger/v1-swagger.generated.json) defines the exact paths, query parameters and 200/201 receipts used here. [API usage](https://docs.gitea.com/development/api-usage/) documents token authentication and Link/X-Total-Count pagination. Repository/issue/PR IDs are decoded as int64 and exposed as decimal strings. Typed schema inputs distinguish optional false from omission. There are no administrator operations, file clones or merge operations.

`schema/` is authoritative. Generated `zz_*.go` files come from public SDK v0.7.8; do not edit them. The independent module has no local replace directives. The runtime and manifest version are 0.1.1. The manifest container tag is an intended build target, not evidence of a published image. Complete English/Chinese guides are wired into the locale catalog with the entire English guide as an exact key. Deployment environment descriptions are documented in both guides; SDK v0.7.8 does not include them in its translatable-string set.

Development checks:

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-plugin-gitea .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`TestREDListRepositoriesPagination` and `TestREDCreateIssueFalseOnWire` first failed behaviorally against a compiling generated stub, then passed with implementation. Additional tests exercise actual request bytes, generated credential/input/output dispatch, precise IDs, Unicode names, malformed success bodies, pagination boundaries, error redaction, redirect rejection and non-replayable writes. See [fixture provenance](testdata/README.md).

`TestLocalServiceAcceptance` is skipped unless `SOKEL_TEST_GITEA_URL`, `SOKEL_TEST_GITEA_TOKEN`, `SOKEL_TEST_GITEA_OWNER` and `SOKEL_TEST_GITEA_REPO` configure a caller-created loopback fixture. `SOKEL_TEST_GITEA_WRITE=1` explicitly enables two issue creations and a comment; optional `SOKEL_TEST_GITEA_PR_HEAD` and `SOKEL_TEST_GITEA_PR_BASE` also enable one PR creation. These branches must already differ. Do not rerun PR creation against an existing open head/base pair. The caller owns cleanup. The isolated Gitea 28.3.0 run passed under the race detector, including issue pagination and PR creation. No production account, token or repository was used.
