# Gotenberg plugin

Four typed operations connect platform files to a self-hosted Gotenberg 8 service: health, HTML conversion, office conversion and ordered PDF merging. Configure `SOKEL_GOTENBERG_URL`; no hosted API or API subscription is required. Read [the user guide](docs/gotenberg.md) or [中文指南](docs/gotenberg.zh-CN.md).

The module uses the public Sokel SDK v0.7.8 with no replacement. `schema/schema.go` is the declaration; generate the `zz_*.go` files with the pinned generator. The manifest comes from real SDK export plus publication-independent metadata and the required deployment URL. English is the default. The Chinese locale includes every supported UI string and the exact complete English guide as its document key. SDK 0.7.7 does not localize deployment-environment descriptions; the full bilingual guide covers configuration.

Design points:

- SDK `File.Blob` and `Ctx.Upload` connect inputs/outputs to the workspace file store. No input path is opened and no user URL is fetched.
- Multipart uses ordinary quoted UTF-8 filenames with escaped quotes, not `filename*`. Merge inputs are numbered before upload to preserve array order under upstream filename sorting.
- HTTP Basic credentials apply only to the configured origin/prefix. Redirects, cookie jars and automatic application retries are disabled; write bodies cannot be replayed with `GetBody`.
- Bounds apply before and after file retrieval, to the complete multipart request and to the PDF response. PDF header/EOF checks are sanity checks, not a parser implementation.
- Chromium may fetch resources referenced inside HTML. Gotenberg outbound policy and container isolation remain the operator's responsibility; credentials are not passed as browser headers.

Validation:

```sh
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-gotenberg .
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

The contract and generated shell preceded implementation. Three compilable behavior tests initially failed with `not implemented`, then passed after implementation. Tests inspect multipart bytes, Chinese/quoted filenames, explicit false values, input ordering, platform dispatch, auth, redirects, limits and storage failures. Synthetic `%PDF` test data tests transport validation only and is not a real rendered PDF. See [fixture provenance](testdata/README.md). Real container acceptance is reported separately; fixture tests do not establish live rendering quality.

Upstream reference: [Gotenberg v8.39.0](https://github.com/gotenberg/gotenberg/releases/tag/v8.39.0), MIT license for Gotenberg code. Chromium, LibreOffice and other packaged engines retain their respective licenses. The plugin does not bundle or deploy those engines.
