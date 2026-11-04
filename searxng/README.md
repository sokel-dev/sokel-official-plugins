# SearXNG plugin

Independent Go module using the public Sokel SDK v0.7.8. Read [the usage guide](docs/searxng.md) or [简体中文](docs/searxng.zh-CN.md) for deployment and operation semantics.

## Scope and design

Three operations: `health`, `get_config`, `search`. Routes and response serialization checked against official rolling source on 2026-11-04. SearXNG is AGPL-3.0 software deployed independently; this connector does not bundle upstream code. The service is self-hostable, while online search engines require external connectivity. No search vendor key or hosted SearXNG account is required by this connector.

Deployment URL comes from `SOKEL_SEARXNG_URL`; optional credentials are for reverse-proxy HTTP Basic authentication only. Paths preserve deployment prefixes. Explicit safe_search=0 is distinct from omission. Search handles the current JSON shape without requiring the removed legacy number_of_results field. The first version projects ordinary result summaries, suggestions, corrections and engine errors, not rich answers/infoboxes. Partial engine failure is surfaced independently of result count. A result limit clips only the current page; the plugin does not invent continuation or a total count.

Both transport and protocol tests verify actual outgoing HTTP requests and the SDK-generated dispatch path. Private HTTP services are supported intentionally. Redirects are rejected, TLS checks remain enabled, responses are bounded, and raw transport errors or provider bodies are not surfaced in runtime errors.

## Validation and development

```sh
GOWORK=off go generate .
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go build -o /tmp/sokel-plugin-searxng .
sokel-gen check .
```

Never edit generated `zz_*.go`. Update schema, regenerate with v0.7.8, implement the typed handler, and keep the source manifest aligned with the real SDK export. English is the source language. `locales/zh-CN.json` includes every supported UI string and the exact full English guide mapped to the full Chinese guide. SDK v0.7.8 does not extract deployment environment descriptions as translation keys; the same configuration is explained in both guides.

Fixtures are documentation/source-derived, not live captures; see [fixture provenance](testdata/README.md). No live SearXNG or search engine acceptance has been performed. This is distinct from a passing mocked HTTP suite.


## Protocol sources

- [Search API](https://docs.searxng.org/dev/search_api.html)
- [Routes and configuration serialization](https://github.com/searxng/searxng/blob/master/searx/webapp.py)
- [JSON response serialization](https://github.com/searxng/searxng/blob/master/searx/webutils.py)
- [Upstream test data](https://github.com/searxng/searxng/blob/master/tests/unit/test_webapp.py)
- [Upstream source and license](https://github.com/searxng/searxng)
