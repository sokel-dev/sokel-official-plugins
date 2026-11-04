# Crawl4AI plugin

Five typed operations for an existing self-hosted Crawl4AI Docker REST server: public health, synchronous crawl, asynchronous crawl submission, one-shot task status and raw Markdown extraction. This is not a Crawl4AI Cloud integration. It needs no SaaS or LLM provider account, and does not install browser components.

This product includes software developed by UncleCode (https://x.com/unclecode) as part of the Crawl4AI project (https://github.com/unclecode/crawl4ai).

## Protocol and evidence

Reviewed on 2026-11-04 against official release **v0.9.5**. The upstream [LICENSE](https://github.com/unclecode/crawl4ai/blob/v0.9.5/LICENSE) is **Apache-2.0 with an additional attribution requirement**, not an unmodified Apache-2.0 license. The attribution above is reproduced as required by that text. This Go HTTP integration does not bundle the Python crawler or a browser image.

Authoritative sources:

- [v0.9.5 release](https://github.com/unclecode/crawl4ai/releases/tag/v0.9.5) and [self-hosting guide](https://docs.crawl4ai.com/core/self-hosting/). The guide's image example still names 0.9.2; protocol decisions here follow the tagged 0.9.5 source.
- [Tagged server](https://github.com/unclecode/crawl4ai/blob/v0.9.5/deploy/docker/server.py): `/health`, `/crawl`, `/md` and the raw Markdown response envelope.
- [Tagged job router](https://github.com/unclecode/crawl4ai/blob/v0.9.5/deploy/docker/job.py): POST `/crawl/job` returns 202 and GET `/crawl/job/{task_id}` queries one job.
- [Tagged API handlers](https://github.com/unclecode/crawl4ai/blob/v0.9.5/deploy/docker/api.py): task ID format, status values, nested completed results, per-page failures, ownership and cleanup.
- [Tagged request models](https://github.com/unclecode/crawl4ai/blob/v0.9.5/deploy/docker/schemas.py) and [configuration trust boundary](https://github.com/unclecode/crawl4ai/blob/v0.9.5/crawl4ai/async_configs.py).

Required deployment URL: `SOKEL_CRAWL4AI_URL`. It accepts internal HTTP and reverse-proxy prefixes with no public fallback. Credentials hold only the self-hosted static API token or an existing JWT. Public health does not validate that token or the browser. Server operators configure `CRAWL4AI_API_TOKEN` independently; otherwise 0.9.x may bind loopback inside its container.

The typed crawl option allowlist contains page timeout, minimum word count, text-only extraction and a CSS selector. The plugin sends `stream=false` and never exposes arbitrary scripts, hooks, browser configuration, cookies, headers, proxy settings or LLM options. `/md` is fixed to `raw`; fit/BM25/LLM paths are excluded, avoiding their eager provider setup in the inspected source. Browser-driven pages still execute their own ordinary site code under the upstream sandbox and egress policy.

Task acceptance, task completion and individual page success are separate signals. Job polling uses only the canonical same-origin endpoint, never response `_links`. Internal exception text, traces, HTML, response headers and unrequested extraction data are discarded. The complete upstream response must still fit 8 MiB. This version allows at most five URLs per request and uses a 150-second HTTP cap; synchronous heavy operations declare 180 seconds. No actual browser container was launched, so website compatibility and runtime resource sizing remain deployment acceptance work.

## Development and validation

The contract lives in `schema/schema.go`. Run real SDK code generation before changing handlers; never edit `zz_*.go`. The independent module pins the public `github.com/sokel-dev/sokel-plugin-sdk v0.7.8` without local replacements. Runtime and manifest versions are both `0.1.1`. The manifest's container reference is the intended release tag, not evidence that an image has been published.

```sh
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen generate .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go test -race ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go vet ./...
GOWORK=off GOCACHE=/tmp/sokel-go-cache go build -o /tmp/sokel-crawl4ai .
GOWORK=off GOCACHE=/tmp/sokel-go-cache go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

`TestDocumentedWireRequests` invokes the actual generated dispatch and checks the HTTP method, path prefix, query, exact JSON body, authentication header and disabled mutation-body replay. These tests were first run against compiling unimplemented handlers and failed for every operation, then passed after implementation. Fixtures are synthetic adaptations of official documentation/source, **not captured live responses**. No real account, production service or browser workload was used. Full provenance is in `testdata/README.md`.

Further tests exercise application-level malformed responses, explicit false/zero values, canceled contexts, timeouts, oversized messages, content types, redirect rejection, HTTP errors and credential redaction. The plugin does not retry writes. Runtime errors do not reflect upstream bodies or request URLs. Service endpoints are deployment configuration rather than per-operation input or identity data.

User documentation is in `docs/crawl4ai.md` and `docs/crawl4ai.zh-CN.md`. `locales/zh-CN.json` translates all exposed UI text and maps the exact complete English guide to the complete Chinese guide. Update both guide files and that exact translation key together. The deployment environment description is English manifest metadata; SDK 0.7.7 does not localize that field, and the bilingual guides explain it fully.
