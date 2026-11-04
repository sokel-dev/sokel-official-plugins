# Apache Tika plugin

Four typed operations extract text, container metadata and recursive content, or detect media type, through a self-hosted Apache Tika Server. Configure `SOKEL_TIKA_URL`. The contract explicitly targets **Tika 4.2.0**, including `/detect`, `/tika/json/text` and `tk:*` metadata. Tika 3.x compatibility is not claimed. Read [the user guide](docs/apache-tika.md) or [中文指南](docs/apache-tika.zh-CN.md).

This standalone module depends only on public Sokel SDK v0.7.8 and its indirect dependencies. `schema/schema.go` declares all fields before code generation. Keep generated files untouched. The real-export manifest includes the required deployment URL; English defaults and complete Chinese UI/full-guide translations are provided. SDK 0.7.7 excludes deployment descriptions from translation keys, so the bilingual guide provides the translated configuration instructions.

Protocol and boundary decisions:

- Inputs are platform files read through SDK `File.Blob`; raw bytes are uploaded using PUT. Local paths, URL-only references and arbitrary server endpoints are not accepted.
- Text extraction uses JSON metadata rather than a plain-text endpoint so a 200 response with partial-result markers can fail explicitly. Any nonempty exception/warning marker except `false` fails the entire result, including embedded documents.
- Metadata is represented as typed `name` / `values` entries. JSON scalars/structures become compact strings; string arrays remain multi-value entries. Numeric text never passes through float64. The `tk:content` field is separated as text.
- Basic auth is an optional reverse-proxy mechanism, not an invented native Tika account scheme. Credentials are not logged, redirected or placed in URLs. No raw upstream error is returned.
- Operations are bounded to 32 MiB input, 16 MiB extraction output, 10000 recursive entries and a 240-second transport timeout within a 300-second operation deadline. The configured server controls its own parser/OCR/resource settings.

Validation:

```sh
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-apache-tika .
GOWORK=off go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

Three behavior tests against a compiling generated shell first failed with `not implemented`. Implemented tests verify raw request bytes, filename headers, optional auth, redirects, exact integer metadata, legacy/partial response rejection, recursive children and generated dispatch. The fixtures are synthetic adaptations of official endpoint and serializer contracts, not captured service responses; [provenance](testdata/README.md) distinguishes them. Real container acceptance is reported separately. No public deployment is claimed.

Apache Tika is Apache-2.0 licensed; its [LICENSE](https://github.com/apache/tika/blob/4.2.0/LICENSE.txt) and NOTICE cover bundled components. This plugin does not bundle the Tika server or require a commercial service.
