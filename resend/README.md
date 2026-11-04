# Resend plugin

Eight operations cover transactional sending, scheduled-email cancellation, sent/received email reads and domain inspection. See the [user guide](docs/resend.md) and its [Chinese translation](docs/resend.zh-CN.md).

## Implementation

The Go `schema/` package is authoritative for typed operations and credentials; `manifest.yml` carries exported fields plus release metadata. SDK v0.7.7 generates `zz_*.go`; never edit those files. Nested pointer fields preserve null responses and an explicitly empty plain-text body. Pagination is forward-only through UUID cursors, capped at 100 records per request. No provider URL is followed and redirects are disabled.

Writes are never automatically retried. The sender-provided idempotency key is transmitted in a header, preserving Resend's 24-hour deduplication behavior. Reads return typed delivery events rather than claiming that acceptance is delivery. The connector does not make a send-only credential look invalid by automatically requiring domain-list permissions. It registers no credential probe or webhook source.

Request bodies are bounded at 1 MiB and responses at 8 MiB. Received-mail queries request CID-form HTML; raw mail and attachment files are not downloaded. Error messages disclose HTTP status and retry delay without including tokens or reflected provider bodies. Interfaces and default docs are English; `locales/zh-CN.json` includes every display string and the full English guide as an exact translation key.

## Validation

```sh
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 generate .
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 check .
go test -race ./...
go vet ./...
go build -o /tmp/sokel-plugin-resend .
```

Tests use official documentation examples identified in [testdata/README.md](testdata/README.md), with synthetic failures and boundary variants kept in test code. Requests are recorded through an in-memory HTTP transport; no API key, account or actual message is used. This does not establish live delivery or account entitlements. Source image metadata names an intended release artifact; no image or marketplace version has been published in this batch.

## References

- [Send email and idempotency](https://resend.com/docs/api-reference/emails/send-email)
- [Read sent email](https://resend.com/docs/api-reference/emails/retrieve-email) and [list sent email](https://resend.com/docs/api-reference/emails/list-emails)
- [Cancel scheduled email](https://resend.com/docs/api-reference/emails/cancel-email)
- [Read received email](https://resend.com/docs/api-reference/emails/retrieve-received-email) and [list received email](https://resend.com/docs/api-reference/emails/list-received-emails)
- [Read a domain](https://resend.com/docs/api-reference/domains/get-domain) and [list domains](https://resend.com/docs/api-reference/domains/list-domains)
