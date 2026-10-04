# linkedin — LinkedIn personal update publisher (P1)

Uses the same publish contract as the three P0 publishers. The user-facing manual is
[docs/linkedin.md](docs/linkedin.md).

## Why only personal profiles

A personal update goes through the self-serve "Share on LinkedIn" product + `w_member_social`,
**available immediately, no approval needed**; a company page requires `w_organization_social` +
the partner program, with an approval cycle measured in weeks to months. Cramming both into one
plugin would turn "why can't I post to my company page" into a question that can never be
explained well — when a company page is actually needed, a separate plugin should be built, since
its credential, scopes, and approval status would all differ from this one.

## Four design decisions

1. **Both headers must be sent**: `LinkedIn-Version` (YYYYMM) and
   `X-Restli-Protocol-Version: 2.0.0`. Missing either one means a 426 or a mismatched shape, and
   the error message won't tell you a header is missing. The version is **pinned** in
   `linkedInVersion` — LinkedIn releases one every month and deprecates old ones roughly a year
   later; upgrading should be a deliberate action.
2. **author must be the account's own URN** (`urn:li:person:{sub}`, fetched from
   `/v2/userinfo`). It never changes, so it's cached by access_token — asking again on every post
   would be pure waste.
3. **On a successful post, the id is in the `x-restli-id` response header**, and the body can be
   empty. Only parsing the body would produce "it succeeded but there's no id", leaving downstream
   with neither a link nor a way to delete it.
4. **Images are a three-step process**: `initializeUpload` gets an upload URL and a URN → PUT the
   binary to the **temporary URL** LinkedIn hands back (not the API domain, no version header) →
   splice the URN into the post. A single image goes through `media`, multiple images go through
   `multiImage`.

## A 60-day token, and most apps get no refresh token

A self-serve app only gets a 60-day access_token; a refresh_token has to be separately applied for
(the Marketing Developer Platform). So the platform-side `linkedinOAuth` **doesn't treat "no
refresh_token" as a dead end the way it does for Google** — doing so would mean no self-serve
credential could ever finish authorizing; it follows the Notion path instead, where the
access_token lands directly in the credential.

Expiry doesn't fail silently: `health_check` marks it invalid, which triggers the "credential
invalid" alert (see `docs/dev-playbook.md` §4.4). The 401 error message names the likely cause
directly: "most likely expired after 60 days".

## Files

| File | What it does |
|---|---|
| `schema/schema.go` | Operation/credential/auth contracts (**source of truth**; run `go generate` after editing) |
| `post.go` | Outbound calls (two headers + error translation), account URN caching, post/delete/health check, the three-step image upload |

## Platform-side integration points (added by this plugin)

- `server/internal/credential/oauth.go`: a `linkedin` implementation added to the `providers`
  table;
- `server/internal/config/config.go` + `internal/api/server.go`: the four `LINKEDIN_OAUTH_*`
  environment variables;
- `server/internal/api/seed.go`: a directory entry + the count in `plugin_test.go`.

## Development

```bash
go generate ./... && go build ./... && go vet ./... && go test -race ./...
```

The tests (against a fake LinkedIn) pin down exactly the four points above + the 401 message +
input guards + converting a post link to a URN.

## Not done

- **Company pages** (see above), **@mentions** (LinkedIn needs a URN, not an @name, and automation
  can easily @ the wrong person), **video** (a separate chunked-upload + ETag flow; will be added
  once there's a use case).
