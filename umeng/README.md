# umeng — Umeng push plugin (first-party, self-hosted)

4 operations: push (unicast/listcast/broadcast), task_status, cancel, health_check.
Usage doc: [docs/umeng.md](docs/umeng.md). Interface details are modeled on a push
service already running in production.

## Gotchas (read before touching the code)

- **The signature is MD5("POST" + full URL + body + master_secret), appended as
  ?sign=** — the URL does not include ?sign= itself; the body is the serialized text
  as-is (changing a single byte breaks the signature).
- **Android and iOS payload shapes are completely different**: Android uses its own
  format, {display_type, body:{title,text}}; iOS uses APNs' {aps:{alert:{}}}, with
  custom keys sitting alongside aps.
- **Unicast is message-class**: Umeng gives it no task statistics, returning msg_id
  instead of task_id; querying its status returns 2000.
- health_check queries the status of a nonexistent task ID: wrong credentials return
  1002/1003, correct credentials return "task not found" — this verifies the
  credentials without sending a real push.
- cast type is auto-determined by token count: 1 token = unicast, >1 = listcast,
  0 = broadcast.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```
