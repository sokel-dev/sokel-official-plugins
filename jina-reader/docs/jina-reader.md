# Jina Reader

`read` accepts HTTP/HTTPS page URLs and returns Markdown by default. Select text/html, CSS target/removal selectors or cache bypass as needed. Outputs include source/final URL, title, description, content, publication time and metadata. URLs require a scheme; user information is rejected and fragments are removed.

The API key is optional; anonymous requests have lower rate limits. With a key, `health_check` validates identity using the official read-only `GET /v1/batches?limit=1` without creating a job. This checks identity and basic access, not Reader quota. Anonymous health checks return unverified rather than treating a public endpoint as credential validation.

Deployment variables: `SOKEL_JINA_READER_BASE_URL` defaults to `https://r.jina.ai`; `SOKEL_JINA_API_BASE_URL` defaults to `https://api.jina.ai` for read-only identity checks. HTTPS is required; HTTP is limited to loopback tests. Keys are sent only to deployment-configured services. Target URLs stay in JSON request bodies. Redirects are rejected. Requests time out after 150 seconds and responses are capped at 16 MiB. Errors never echo upstream bodies.

Official specification: https://docs.jina.ai/openapi.json
