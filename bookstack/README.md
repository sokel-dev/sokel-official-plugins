# BookStack plugin

Nine typed operations for self-hosted BookStack knowledge content. Required deployment setting: `SOKEL_BOOKSTACK_URL` (site root with optional prefix); credentials hold only token ID/secret. The stable baseline is **BookStack v26.09.2**, unversioned REST `/api`, MIT licensed. See [the usage guide](docs/bookstack.md) and [中文指南](docs/bookstack.zh-CN.md).

The pinned [page controller](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Entities/Controllers/PageApiController.php) uses 200 receipts for create/PUT update, and allows partial update fields. [PageRepo](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Entities/Repos/PageRepo.php) explicitly clears content when supplied HTML/Markdown is empty. General lists use count/offset; [search](https://github.com/BookStackApp/BookStack/blob/v26.09.2/app/Search/SearchApiController.php) uses count/page and has no list-style sort/filter. Book contents are flattened without losing chapter pages. IDs are parsed as int64 and exposed as strings. Page-update options preserve explicit zero/empty arrays, and input null is omission. No user, permission, deletion or server-administration operation is exposed.

`schema/` is authoritative. Public SDK v0.7.7 generated all `zz_*.go`; no local replace or private module is used. Runtime/manifest version is 0.1.0. The manifest's container tag is an intended build artifact, not proof of publication. Default UI/README/guide are English, all UI and the complete guide are translated in zh-CN. Deployment descriptions are covered by the guides because SDK v0.7.7 does not include those descriptions in its locale key set.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-plugin-bookstack .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`TestREDListOffset` and `TestREDUpdateZeroAndEmptyTags` first failed on compiling generated stubs, then passed with implementation. Tests cover official stable API examples, generated dispatch and actual request bytes plus synthetic malformed/hostile responses. [Fixture provenance](testdata/README.md) distinguishes official examples from synthetic cases. **No live BookStack stack was run**; this is protocol validation, not live or production acceptance.
