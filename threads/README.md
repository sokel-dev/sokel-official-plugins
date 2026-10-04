# threads — Threads (Meta) publisher (the last P1 plugin)

Uses the same publish contract as the other publishers. The user-facing manual is
[docs/threads.md](docs/threads.md).

## Four design decisions

1. **Two-step publishing is wrapped into one operation.** Meta's shape is "create a media
   container to get a creation_id → publish it", but that's an implementation detail of theirs,
   and shouldn't become two nodes on the canvas. The container id is **not** the post id —
   mixing them up means the downstream link can't even be opened.
2. **With media, you have to wait for the container to be ready before publishing.** After
   creating the container, Meta still pulls the file down and processes it in the background;
   publishing immediately gets rejected. Skip the wait and the symptom is **random failures that
   succeed on retry** — the hardest kind to debug. On an ERROR status, Meta's `error_message` is
   carried through as-is (most likely it couldn't download the address).
3. **Media is "let it fetch", not upload.** So what's given must be a publicly reachable absolute
   URL: the platform rewrites file references into signed download URLs when handing them to the
   plugin, `mediaURLs` only accepts ones starting with `http(s)://`, and relative paths are always
   discarded — letting Threads try to download `/api/v1/files/xx` would just produce an unrelated
   error.
4. **Images and video use different parameter names** (`image_url` / `video_url`), and getting it
   wrong just gets "missing required parameter" from Meta, without saying which one you got wrong.
   Decided by extension (after stripping the query string).

Multiple media go through a carousel: each one builds an `is_carousel_item` container first, then
one `CAROUSEL` container strings them together.

## Token and quota

- **Expires in 60 days with no refresh_token** (renewal means exchanging the long-lived token for
  a new long-lived token). The platform-side `threadsOAuth` does **two hops** when exchanging the
  code: auth code → 1-hour short-lived token → 60-day long-lived token. **Doing only the first hop
  means the credential expires in an hour**, and at that point nobody would think to connect it to
  a missing step.
- **250 per 24 hours** (an account-level rolling window). `health_check` surfaces the used/total
  quota along the way, so a workflow can decide whether to keep publishing based on it — cheaper
  than hitting a 429 and retrying.

## Platform-side integration points (added by this plugin)

A `threads` implementation added to the `providers` table in
`server/internal/credential/oauth.go` + the four `THREADS_OAUTH_*` environment variables in
`config.go`/`api/server.go` + a directory entry in `api/seed.go` and a count in `plugin_test.go`.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

The tests (against a fake Threads) pin down exactly the four points above + thread chaining +
quota surfacing + the expired-token message + input guards.

## Not done

- **Delete post**: Threads's delete API isn't stable enough yet — better to not guess at an
  endpoint than to build on a shaky one (delete it from the app if you posted the wrong thing).
- **Reading** (own posts/replies/insights): this plugin's scope is publishing only.
