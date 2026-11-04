# Airtable plugin

Independent Go module using only the public Sokel SDK v0.7.8 and Go's standard library directly. The default contract and user guide are English; Chinese translations are in `locales/zh-CN.json`. `manifest.yml` is the source for generated types and registration code.

## Development

```sh
go generate ./...
go test -race -cover ./...
go vet ./...
go build ./...
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen check .
```

The module has no `replace` directives or platform-internal imports. Runtime version is `0.1.1`. The health operation returns `ok` and `message`, including `ok=false` on failure.

## Contract and upstream details

Operations list bases and table schemas, list/get records, and create/PATCH/delete one record. Explicit offset pagination preserves the upstream cursor without decoding or reconstructing it. IDs are validated before URL construction; this first version deliberately accepts table IDs rather than table names.

Dynamic field values and per-field schema options have opaque-field reasons in the contract. JSON numbers use `json.Number` so large IDs and integer values are not rounded. Read-side missing fields remain absent. Writes preserve zero, false, empty string and null; omitted PATCH fields remain unchanged. Type conversion is opt-in.

Every request is attempted once. The client never automatically replays writes after 429, 5xx or transport failures. Errors retain status and actionable guidance without reflecting response bodies or request URLs. Redirects are rejected. A deployment-only base URL allows trusted gateways and local fixtures without adding routing configuration to credentials.

## Polling design

The optional source compares complete snapshots of one table, with a hard 500-record bound and 250 ms between pages. It emits newly observed records and changes to visible content, but no deletion events or claims about intermediate mutations. The baseline update is published only after a complete scan and successful event publication. Checkpoint data, including the next-poll time, is sent to the platform credential store. SDK 0.7.7 publication is fire-and-forget; local publication does not confirm durable platform storage or downstream acknowledgment. The next-poll time preserves the interval across credential-triggered source restarts.

Event IDs contain the base, table, record, snapshot revision and content hash. A checkpoint retry uses the same IDs; a later change back to earlier content gets a new revision. Incomplete scans and inconsistent pages fail without updating state. Use one active source process per credential; parallel replicas have no compare-and-swap checkpoint coordination.

## Test provenance and remaining acceptance

Compilable stubs initially failed `TestRecordsPaginationAndQuery` and `TestCreateRecordBytes` with `RED: not implemented`; both passed after implementation. Further tests cover all methods, actual JSON and query bytes, integer precision, 429/no retry, redirects, errors, validation, snapshot pagination and checkpoint safety.

The fixtures are synthetic HTTP contracts based on official API descriptions and the official JavaScript client. They are not live account captures. No real PAT was available, so scope authorization and actual base/record operations remain to be accepted against a real account.

Official references checked:

- [API pagination and omitted empty fields](https://support.airtable.com/articles/6292134965-getting-started-with-airtable-s-web-api)
- [Rate limits and the 30-second recovery period](https://support.airtable.com/articles/7735693959-managing-api-call-limits-in-airtable)
- [Official Airtable JavaScript table implementation](https://github.com/Airtable/airtable.js/blob/master/src/table.ts)
- [List bases](https://airtable.com/developers/web/api/list-bases)
- [Get base schema](https://airtable.com/developers/web/api/get-base-schema)
- [List records](https://airtable.com/developers/web/api/list-records)

The developer API pages returned an unsupported Markdown content type in the browsing tool; the accessible official help center and official client were used to cross-check request semantics. The links remain useful for live acceptance.
