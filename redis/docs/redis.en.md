# Redis

Use Redis as **shared memory for workflows**: counters across runs, deduplication sets, state flags, queues, rankings,
and a real-time messaging channel to external systems. Self-hosted and managed instances, including Alibaba Cloud/Tencent Cloud Redis
and Upstash, work the same way; enter the address in the credential.

## Configure credentials

| Field | What to enter |
|---|---|
| Address | `host:port`, such as `redis.internal:6379`. **The plugin process must be able to reach it**. For managed instances, add the plugin machine's public IP to the allowlist. |
| Username | A Redis 6+ ACL user. Leave empty for traditional `requirepass` authentication. |
| Password | Leave empty if none is configured. |
| Database | Defaults to 0. **Cluster mode supports only database 0**. |
| TLS | `off` for plaintext internal connections; `on` for encrypted managed connections; `on_insecure` for self-signed certificates, encrypting without certificate verification. |

After configuration, click Check credentials. A successful connection shows the version, deployment mode, memory, and key count in the current database.

## Choose an operation

- **Store or retrieve a value** → Get / Set value. Set supports an expiration in seconds and a write condition.
  `Only if the key does not exist` provides **an idempotency gate**: proceed only if `ok=true`; a duplicate trigger naturally stops.
- **Count, rate-limit, or allocate sequence numbers** → Increment, an atomic operation that does not lose concurrent updates.
- **Check whether an item was already seen** → Add to set, then inspect `added`: 0 means already present. This saves a round trip
  compared with reading before writing and avoids a concurrency race.
- **Object-like state**, such as several fields for a task → Set / Get hash. Updating one field is easier than modifying a JSON string.
- **Queues** → Push / Pop list. Push right and pop left for FIFO. An empty queue returns an empty array, not an error.
- **Rankings, priorities, or time windows** → Add to sorted set + Read sorted-set range. Top 10 means start 0, count 10, descending scores.
- **Find a set of keys** → Scan keys. **Do not use KEYS**: Redis is single-threaded and KEYS blocks the entire instance.
  Scan incrementally with a cursor. Pass the returned cursor into the next call; scanning finishes only when `done=true`.
- **Commands not listed above** → Generic command. Supply the command name and arguments in the order specified in the official documentation.

## Messaging: Pub/Sub or Stream

Both routes turn external messages into workflow triggers. The difference is **whether message loss is acceptable**:

| | Pub/Sub: Publish message / Channel event | Stream: Add to Stream / Stream event |
|---|---|---|
| Retention | Not retained; discarded after publishing. | Retained and replayable. |
| Messages while the plugin is disconnected | **Missed**. | Can be recovered by resuming from the ID after reconnection. |
| Message ID | None. | Unique and increasing. |
| Best for | Real-time signals such as cache invalidation and reloads. | Tasks that must not be lost, such as orders, alerts, and to-dos. |

To trigger workflows from messages, set Channels to watch or Streams to watch in the credential.
**The event source starts only when configured**. Channels support patterns such as `orders.*`. Streams start reading from the moment of connection,
without replaying history; connecting an old Stream does not flood workflows with its backlog.

## Common pitfalls

- **Missing data is not an error**. Get returns `exists=false` for a missing key; Pop returns zero items for an empty queue;
  a failed Only if absent condition returns `ok=false`. These are normal branches. Check outputs with condition nodes rather than expecting an error.
- **Expiration must be positive**. Redis **immediately deletes the key** for zero or negative expiration. The plugin rejects this; use Delete keys when deletion is intended.
- **Bound writes where possible** using value expiration and the Stream retention-count limit. Unbounded keys keep growing;
  when memory fills, Redis rejects writes or evicts data you did not intend to delete.
- **Values are strings**. Numbers and booleans are returned as strings because that is how Redis stores them.
  Convert them in the workflow if you need numbers.
- **Cluster mode** supports only database 0. Multi-key commands spanning slots, such as deleting several keys at once, may be rejected; split them into single-key operations.
