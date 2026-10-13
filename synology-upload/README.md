# synology-upload — upload files to a Synology NAS through DSM

One operation, `upload_file`, plus the standard `health_check`. The user-facing guide is
[docs/synology-upload.md](docs/synology-upload.md) (embedded and reported in the handshake; it is the "usage doc" in
the UI).

## Why not a write operation on the synology plugin

The synology plugin works on a NAS directory bind-mounted into its container. A file a container writes into a bind
mount lands on disk, but the host never gets a change notification for it, so **Synology Drive / ShareSync never syncs
it** (it shows up once something on the host touches the file, or Drive restarts). This is a known Docker-on-DSM
limitation — see the Synology community threads "Drive ShareSync - not noticing files written from Docker container"
and "Docker bind mount silently drops inotify events, breaking Synology Drive real-time sync".

Uploading through FileStation (`SYNO.FileStation.Upload`) makes DSM write the file itself, so Drive, Cloud Sync and
indexing see it. That needs a DSM account instead of a mounted path and can run wherever the NAS answers — a different
credential and deployment shape, hence a separate plugin rather than a mode on the watcher.

## Wire notes (pinned in main_test.go)

- **Login** `POST /webapi/auth.cgi`, `SYNO.API.Auth` v6 (accepted by DSM 6.x and 7.x), `session=FileStation`,
  `format=sid`. The password goes in the form body, never the query string: URLs end up in DSM's and proxies' access logs.
- **Upload** `POST /webapi/entry.cgi?api=SYNO.FileStation.Upload&version=2&method=upload&_sid=…`, multipart with
  `path`, `create_parents`, optional `overwrite`, then the `file` part **last** — DSM reads the fields before the file.
- **Conflict modes** map onto `overwrite`: overwrite → `true`; skip → `false` (DSM answers success with `blSkip:true`);
  error → field left out (DSM refuses an existing name: 414 on the DSM 7 tried, 1805 per the guide; both are translated).
- **One session per call**, logged out in a `defer`, so a failed upload still closes it. A cached sid would need expiry
  and duplicate-login handling for no real gain at this call rate.
- **A lost session gets one fresh login.** DSM sometimes answers an upload with 119 (session not found) while uploads
  run side by side — seen on a real DSM 7 in the first batch run, not reproduced on demand. On 106 / 107 / 119 the call
  logs in again once and sends the file again; a second loss fails the call.
- The multipart body is streamed through a pipe while it is sent; the file bytes are already one full copy in memory
  (the SDK's `Fetch` returns the whole file), a buffered body would make it two.
- Error codes follow Synology's DSM Login and File Station API guides (`authErrText` / `uploadErrText`).

## Develop

```
go generate ./...        # schema/ → zz_*.go (with GOFLAGS=-mod=mod on a fresh checkout)
go test -race ./...
SOKEL_ENDPOINT=https://<platform> SOKEL_TOKEN=skp_xxx SOKEL_INSTANCE_ID=<name> go run .
```
