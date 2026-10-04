# kubernetes — generic K8s plugin (first-party self-hosted)

8 operations: pods (with an abnormal-only view), pod logs (including the previous instance),
deployments, rolling restart, scale replicas, events (Warning first), nodes (with a not_ready
count), health_check (/version). The user-facing guide is
[docs/kubernetes.md](docs/kubernetes.md).

## Architecture decisions

- **Not tied to any cloud**: the credential is just a kubeconfig. ACK's can be one-click
  exported via the aliyun plugin's ack_kubeconfig; self-hosted or other managed clusters work
  the same way.
- **client-go only uses two low-level packages**: `clientcmd` (parses kubeconfig:
  certs/token/exec plugins) + `rest.TransportFor` (gets a RoundTripper already set up with
  mTLS/Bearer). Resource reads/writes are plain REST, with 7 hand-built paths -- the typed
  clientset family isn't pulled in (hundreds of generated types vs. our 7 paths). This matches
  the SDK admission bar set by the Feishu precedent: hand the SDK only the part (auth) that
  isn't realistic to write raw.
- **Write operations only cover the two everyday ones**: rolling restart (= kubectl rollout
  restart, changes the template annotation) and scaling replicas (PATCH /scale, reading the
  previous value first -- both auditing and rollback need "from N to M"). High-risk actions like
  deleting a pod or draining a node aren't included; the permission control point is cluster
  RBAC.

## Gotchas (read before touching the code)

- **client-go silently drops the token for a plain http:// server** (it won't send credentials
  in the clear). Real clusters are always TLS so this never comes up there; **the fake API
  server in tests must use NewTLSServer + insecure-skip-tls-verify**, otherwise a test asserting
  "was the auth header sent" silently tests nothing -- this has bitten us before.
- A multi-container pod without a specified container name gets K8s's "a container name must be
  specified" error -- already translated into plain language.
- K8s returns events in ascending time order, most recent last -- reversed before output, since
  someone looking at events wants the recent ones.
- Clients are cached by kubeconfig content hash: both the TLS handshake and exec credential
  plugins are expensive.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

### Anomaly event monitoring: why these three fields

`pods`'s `last_terminated_reason` / `last_exit_code` / `last_terminated_at` exist on their own
because **OOMKilled only shows up in `lastState`**: the container is restarted immediately after
being killed, so the current `state` is already Running, with nothing visible in `reason`.
Reporting only `restarts` tells the caller it's restarting repeatedly, but not whether it's out
of memory, crashed, or evicted -- these three need completely different handling.

With multiple containers, take the most recently terminated one: reporting the earliest one
would point someone at a problem that's already fixed.

`events`'s `since` compares by **lastTimestamp** (most recently occurred), **not
firstTimestamp**. k8s doesn't create a new entry for a repeated event, it increments `count` and
updates `lastTimestamp` -- comparing by first occurrence would mean a failure lasting two hours
only gets reported in the first round, and forever looks older than the cursor after that
(symptom: the alert fires once and never again).

All three time fields must be recognized (`lastTimestamp` / `eventTime` / `firstTimestamp`):
clusters on the newer `events.k8s.io` only provide `eventTime`, and recognizing only one field
would make filtering permanently ineffective on that half of clusters, with "most recently
occurred" left empty -- which is exactly the field the caller uses as a cursor.

`truncated` exists because of a past silent truncation: when matches exceeded `limit`, they used
to just get dropped without a word, which would quietly drop alerts whenever the cluster got
busy.

### The event source's four disciplines

Shares its structure with the GitHub event source (which hit these first); each corresponds to
a way of "not erroring but being useless":

1. **The first round doesn't trigger.** What's fetched the first time is all of the cluster's
   current historical anomalies; sending them as-is would flood the workflow with dozens of
   stale alerts in the plugin's first minute -- the user's first reaction would be to turn the
   trigger off, which is worse than not having it.
2. **Crashes are deduped by finish time.** The same crash gets seen for several rounds in a row
   before the Pod is replaced; without dedup that's one report per minute.
3. **Nodes are only reported on a status change.** A node can stay NotReady indefinitely, and
   reporting every round would flood hundreds of reports. The record must be cleared on
   recovery, otherwise a second failure goes unnoticed.
4. **Report the Ready condition's raw value, not a boolean.** `False` (the node self-reports
   unhealthy) and `Unknown` (kubelet lost contact) collapse to the same thing as a boolean, but
   one means go look at what's happening on the node and the other means first confirm the
   machine is even there.

**The three poll functions accept the interface `plugin.SourceCtx`, not the struct
`sokel.SourceCtx`.** The struct can't be faked, so accepting it would leave the polling path
with zero test coverage -- that's exactly what happened with the GitHub one, which only ended up
testing its webhook (which happens to accept an interface). Narrowing to the interface is what
makes it testable.

**Crash detection is based on Pod status, not Event**: OOMKilled mostly doesn't emit an Event.

## Five deploy/job operations (workload_ops.go, added 2026-08-29)

- All write paths uniformly use **server-side apply** (PATCH + `application/apply-patch+yaml`,
  fieldManager=sokel): idempotent, so a workflow re-run won't blow up on "already exists";
  created/configured is distinguished by probing with a GET before the apply.
- kind -> REST resource name is **not guessed by pluralization**; it's looked up from the
  `/apis/<gv>` discovery resource table and cached per client+gv (subresources like
  `deployments/scale` are skipped).
- deploy_workload's `app` label is the selector anchor: any `app` in the user's labels gets
  discarded -- the selector is immutable once created, and apply would just be rejected.
- run_job: `backoffLimit=0` (no retry on failure; retry semantics are left to the workflow
  layer), `ttlSecondsAfterFinished=3600`, and the wait cap is capped at 570s (the operation's
  TimeoutSec of 600 leaves 30s to fetch logs); failing to get logs isn't an error (the log is a
  garnish, not the main course).

## Not done

- **A watch long-lived connection**: second-level latency with no gaps, but it has to handle
  reconnects after disconnection and 410 Gone (on resourceVersion expiry you must re-list then
  re-watch, otherwise you silently miss every change in between). Polling gets the pipeline
  working first.
