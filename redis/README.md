# redis — Redis plugin (first-party built-in directory)

23 operations covering six data structures (string/hash/list/set/sorted set/Stream) + a `call` fallback +
`health_check` (PING + INFO), plus event sources (Pub/Sub subscription, Stream consumption). Works with
both self-hosted and cloud-hosted instances (fill in the address in the credential). User docs:
[docs/redis.md](docs/redis.md).

The client is `github.com/redis/go-redis/v9` (RESP protocol, can't be faked with plain HTTP).

## Connection layer (client.go)

**One client per credential, reused for the whole process**: go-redis already pools connections, and
building a new client on every call would mean a handshake + AUTH (+TLS) before every single command —
under heavy write load that overhead would dwarf the commands themselves. The cache key is a credential
fingerprint (first 8 bytes of sha256), not the plaintext — a map key would show up in a crash dump. The
fingerprint is computed from all five of addr/user/pass/db/tls; leaving one out would cause collisions
across someone else's database (tests pin this down).

## Event sources (events.go)

Both paths coexist; whichever the credential fills in is the one that starts:

- **Pub/Sub** (watch_channels): real-time, not retained, **no message id**. event_id can only be
  generated as "the Nth message in this process" — **can't use a content hash**: sending the same
  message twice in a row on the same channel is legitimate (`tick`, `reload`), and deduplicating by
  content would swallow the second one. Numbering resetting on restart doesn't matter, since Pub/Sub
  never redelivers anyway. Channels with wildcards go through PSUBSCRIBE, the rest through SUBSCRIBE;
  kept separate so the `pattern` output only has a value when it was actually subscribed by wildcard.
- **Stream** (watch_streams): a blocking XREAD with `BLOCK 5s` rather than 0 (a permanently blocked
  connection would only wake up when the connection itself is closed, and the process wouldn't exit
  cleanly when ctx is canceled). The cursor starts at `$`, meaning only new messages from the moment it
  connects, with no history replay (matching the same first-run convention as feed/gitlab). event_id =
  `<stream>:<message id>`; XADD's id is unique and increasing, so even a re-read after a restart is
  deduped by the platform.

## Gotchas (read before touching the code)

- **"Not found" is not an error**: a get miss, an empty list_pop queue, an nx that didn't win — all
  return an output field (exists/count/ok), never an error. Throwing an error would fail the whole run on
  the canvas, when it should have been a normal branch. Tests pin this down.
- **The contract uses "how many to take" rather than Redis's native "end index"**: an empty numeric field
  arrives as 0, and `stop=0` in Redis means "just the first one" — if empty can't be told apart from 0,
  it would quietly return only one item. `count=0` has no ambiguity (taking 0 items is meaningless), so
  treating it as "unlimited" is safe. Same reasoning applies to zset_range.
- **EXPIRE with a non-positive number deletes the key immediately**: the plugin blocks this upfront and
  tells people to use "delete key" instead, otherwise a "renewal" written as 0 ends up deleting data.
- **`INFO server memory` is the multi-section form only supported from Redis 7.0 onward**, and errors
  outright on 6.x — the health check fetches the full default output with no arguments. The INFO parsing
  fixture uses **actual output from a real instance** (miniredis's INFO is incomplete, and using it as
  the fixture would leave that branch permanently green).
- **TTL goes through the raw `Do("TTL")`**: go-redis's Duration form expresses -1/-2 as negative
  nanoseconds, which is easy to get wrong converting back to the sentinel values, and -1 (permanent) / -2
  (doesn't exist) are exactly what this operation needs.
- **Written values are always stringified** (`asString`): a JSON number like 1000000 shouldn't come out
  as `1e+06`, which is float64's default `%v` behavior.
- Cluster mode only has db 0, and multi-key commands spanning slots may be rejected — this is documented
  in the user docs.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

Tests use miniredis to spin up an in-process instance and exercise every operation path, with no
dependency on an external service.
