# Fixture provenance

Retrieved on 2026-11-04 from the [official Twilio OpenAPI definition](https://github.com/twilio/twilio-oai/blob/main/spec/yaml/twilio_api_v2010.yaml). JSON files contain complete published examples, reformatted only. These are documentation examples, not live-account captures.

| Fixture | Operation | OpenAPI example |
| --- | --- | --- |
| `account.json` | FetchAccount | fetch |
| `message.json` | FetchMessage | fetch |
| `messages.json` | ListMessage | readFullPage1 |
| `send.json` | CreateMessage | create |
| `number.json` | FetchIncomingPhoneNumber | fetch |
| `numbers.json` | ListIncomingPhoneNumber | readFull |
| `media.json` | ListMedia | readFull |

Official account examples contain the literal placeholder `auth_token`; tests assert this field is excluded from connector outputs. There are no real credentials. Test code creates separate synthetic pagination, error, invalid-input and hostile-URL cases.
