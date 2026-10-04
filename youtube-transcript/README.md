# youtube-transcript

A first-party plugin for fetching YouTube transcripts. The fetching approach is modeled on the Python
[jdepoix/youtube-transcript-api](https://github.com/jdepoix/youtube-transcript-api), rewritten in Go with
operation boundaries re-cut to match this platform's contract.

User-facing docs live at [docs/youtube-transcript.md](docs/youtube-transcript.md) (embedded into the
binary and shown on the plugin's detail page). **This file is for whoever is changing the code.**

## How it fetches transcripts

Three steps, all using the undocumented API the YouTube web client itself relies on:

```
① GET  youtube.com/watch?v=<id>              → regex-extract INNERTUBE_API_KEY
② POST youtube.com/youtubei/v1/player?key=…  → captions.playerCaptionsTracklistRenderer
③ GET  <captionTrack.baseUrl>                → timedtext XML, the transcript content
```

Translation works by appending `&tlang=<code>` to the ③ URL, with YouTube doing the machine translation
itself.

**Why not the YouTube Data API v3**: it needs a key, has a quota, and `captions.download` only works for
your own channel's videos — any other video gets a flat 403, meaning the official API simply can't do
this job at all.

## Everything that can go stale lives in one place

The constants at the top of `youtube.go` are this plugin's entire fragile surface:

- `innertubeContext` — spoofs the `ANDROID` client. The web client has started requiring a PO Token in
  recent times, while the Android path still doesn't. **The version number does go stale**; the symptom
  is the player endpoint starting to demand a PO Token.
- `apiKeyRe` / `consentRe` — regexes that scrape things out of HTML. They break whenever YouTube changes
  its page structure.
- `watchURL` / `innertubeURL`

Changing these doesn't require touching anything else. Conversely, when a bug report says "can't find
InnerTube key" or "failed to parse player response", look here first.

## Code layout

| File | Responsibility | Testable? |
|---|---|---|
| `parse.go` | Pure functions: id extraction, XML parsing, track selection, text joining | **Yes**, fully covered by `parse_test.go` |
| `youtube.go` | HTTP + anti-bot detection + error translation | No (needs real network) |
| `ops.go` | Wiring for the three operations, deliberately kept thin | — |
| `schema/` | Contract declarations; run `sokel-gen generate .` after editing | — |

It's split this way because the parts that are actually easy to get wrong happen to be exactly the parts
that can be tested offline:

- **Id shapes**: users paste straight from the address bar. Only recognizing `watch?v=` would reject the
  `youtu.be` links shared from phones, Shorts links, and live-replay links, while the user is staring at
  a link they're sure is perfectly valid.
- **Strip tags first, then unescape entities**: the other way around, content that literally writes
  `&lt;b&gt;` would first become `<b>` and then get deleted as a tag, costing the user part of their
  original text. Tests pin this down.
- **Track-selection order is "language before kind"**: `zh-Hans,en` means Chinese matters more than
  English, so when a Chinese machine translation exists, it shouldn't be skipped in favor of English just
  because "English has a manual transcript".

## Changing the contract

```bash
sokel-gen generate .        # or, from this directory, go generate ./...
```

This generates `zz_types.go` / `zz_register.go` / `zz_credential.go` — don't hand-edit them.

## Running locally

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## Known gaps

- **Age-restricted videos**: require login. The reference project's cookie-auth path has already been
  broken by YouTube, so this is **deliberately not implemented** — keeping it would only mislead people
  into thinking it works.
- **Videos requiring a PO Token** (URL contains `&exp=xpe`): detected and reported explicitly, rather
  than degrading into a bare "XML parse failed".
- **Datacenter IPs**: will hit a 429 or "confirm you're not a bot". The only fix is configuring a
  residential proxy in the credential, and this must be spelled out clearly in the user docs, otherwise a
  user will just see "rate limited" and assume the plugin is broken.
