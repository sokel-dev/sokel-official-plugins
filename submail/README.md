# submail — SUBMAIL SMS plugin (first-party self-hosted)

7 operations: domestic SMS send/template-send/check-delivery-status, international SMS
send/template-send, check balance, health_check. The user-facing guide is
[docs/submail.md](docs/submail.md). A plain HTTP form API, no SDK.

## Design decisions

- **The credential is two key pairs**: in the SUBMAIL console, domestic "SMS" and
  "International SMS" are two separate apps, with two separate appid/appkey pairs. All four
  credential fields are optional; `appOf()` picks the matching group per operation, and a
  missing group gets a pointed error -- instead of sending the domestic key to the
  international endpoint and getting back a 101.
- **Auth uses the plaintext appkey mode** (signature = appkey): the connection is HTTPS end to
  end, so the "eavesdropping in transit" that digest signatures defend against doesn't apply
  here, while that mode would trade in the fragility of clock-synced timestamps.
- **Two kinds of errors are rejected up front**: domestic content missing a 【signature】
  (carrier rejection would otherwise only show up once the delivery report comes back), and an
  international number missing its + country code. Both are caught in the plugin rather than
  discovered only after hitting SUBMAIL.

## Gotchas

- The response's balance is a **string number** ("12345"), parsed with json.Number.
- Error codes 101-104 all mean "wrong key"; the most common actual cause is **putting the
  domestic key into the international group** -- the translation calls this out explicitly.
- health_check queries whichever group is configured, and says clearly which one is broken if
  one is.
- **Delivery-status queries only exist on the v4 gateway** (api-v4.mysubmail.com/sms/log); the
  old gateway returns Unknown method. International SMS has no corresponding endpoint (confirmed
  by testing). "Accepted the submission" does not mean "reached the phone" -- dropped+report is
  the real answer for a message that didn't arrive.
- **The balance endpoints differ by domestic/international**: `/balance/sms` (by message count)
  vs. `/balance/internationalsms` (by amount). Hitting the domestic endpoint with the
  international key misjudges a valid credential as broken -- this bit us once, and a test now
  pins it down.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
