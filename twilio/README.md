# Twilio plugin

Seven typed operations cover SMS/MMS sends, account and message reads, account phone numbers and message media metadata. See the [user guide](docs/twilio.md) and [Chinese translation](docs/twilio.zh-CN.md).

## Implementation

The `schema/` package declares the contract before implementation. `manifest.yml` stores an exported contract with release metadata; `zz_*.go` is generated with public SDK v0.7.7. The module has no local replacements or private dependencies. This first version uses account SID/Auth Token Basic authentication against fixed US1 `api.twilio.com` endpoints.

The account endpoint returns an Auth Token, so its wire response is decoded directly into a restrictive output type that excludes that field. Message SIDs accept both `SM` and `MM`, as demonstrated by Twilio's official history examples. Prices and numeric-looking strings remain strings, optional values remain pointers, and explicitly false Smart Encoding is sent as `SmartEncoded=false`.

Twilio accepts form data, not JSON: repeated `MediaUrl` values are preserved. POST responses must be HTTP 201; reads require HTTP 200. Sends have no automatic retries or invented idempotency. Pagination accepts only a relative URI for the same account and collection, validates query keys and bounds, then reconstructs the request at the fixed origin. Redirects are disabled. Media binaries, phone provisioning, calls and webhook triggers are outside this version's contract.

## Validation

```sh
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 generate .
go run github.com/sokel-dev/sokel-plugin-sdk/cmd/sokel-gen@v0.7.7 check .
go test -race ./...
go vet ./...
go build -o /tmp/sokel-plugin-twilio .
```

[Test fixtures](testdata/README.md) come from Twilio's official OpenAPI examples; malformed and hostile variants are constructed in tests. Tests record HTTP requests without a live account or sending real messages. They cover encoding, SDK binding, false/zero semantics, pagination, credential containment and bounded error behavior. Live account and carrier acceptance remains separate. Image metadata describes an intended artifact, not an already published image.

## References

- [Messages](https://www.twilio.com/docs/messaging/api/message-resource)
- [Accounts](https://www.twilio.com/docs/iam/api/account)
- [Account phone numbers](https://www.twilio.com/docs/phone-numbers/api/incomingphonenumber-resource)
- [Message media](https://www.twilio.com/docs/messaging/api/media-resource)
- [Official OpenAPI](https://github.com/twilio/twilio-oai/blob/main/spec/yaml/twilio_api_v2010.yaml)
