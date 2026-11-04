# YouTube Transcripts

Retrieve captions from public YouTube videos and return both **timestamped segments** and **combined full text**. No YouTube account or API key is required.

## Capabilities

- Select captions by language priority, such as Chinese first and English if unavailable.
- Distinguish **human-created captions** from **automatically generated captions**. Quality can differ considerably, and you can require human-created captions.
- Translate captions into another language using YouTube's built-in machine translation.
- List a video's available caption tracks before choosing one.

Typical flow: retrieve captions → summarize with an LLM → write to a data table or send to a group.

## Two operations

### Get transcript

| Input | Description |
|---|---|
| **Video** | A video ID or YouTube URL. `watch?v=…`, `youtu.be/…`, Shorts, `embed/…`, and recorded livestream URLs are supported. Paste directly from the address bar |
| **Language priority** | Comma-separated languages from highest to lowest priority, such as `zh-Hans,zh,en`. The first match is selected; empty defaults to `en`. **Regional variants are supported**: `en` also matches `en-US` or `en-GB`, so you do not need to inspect the exact track code first |
| **Caption type** | Any, preferring human-created tracks within the same language; human-created only; or automatically generated only |
| **Translate to** | A language code enables YouTube machine translation. **A track is first selected by priority, then translated** |
| **Keep formatting tags** | Preserve inline tags such as `<b>` and `<i>`; removed by default |

The full-text output combines everything into one passage for an LLM. Segments include start time and duration for timeline navigation or time-based slicing. Both are returned because these two uses are equally common; returning only one would force the other use case to make another request.

> Coverage duration is the end time of the final caption, **not the total video duration**. Ending sequences often have no captions.

### List available captions

Given a video, list caption languages, which tracks are human-created, and the languages each track can be translated into.

A common flow uses a condition: if human-created captions are available, retrieve them; otherwise fall back to automatically generated captions. Human-created tracks appear first in the list.

## Deployment and credentials

The plugin **does not require a YouTube account**. Its two credential fields address the same issue:

**YouTube blocks data-center IP addresses.** On a cloud server, you may eventually see a YouTube risk-control error: HTTP 429 or a request to confirm that you are not a bot. The plugin has not necessarily broken; your outbound IP has been classified as a crawler.

Configure a **residential proxy** in the credential:

```
http://username:password@host:port
socks5://host:1080
```

Then use “Test” on the credential page. It makes a real retrieval attempt against a public video; success confirms that the outbound route works.

The other field, `User-Agent`, can remain empty. Set it only when a specific browser identity is needed.

## Reasons retrieval may fail

The plugin reports the reason directly:

| Message | Meaning |
|---|---|
| Captions are disabled for this video | The creator disabled captions, or the video was just published and captions have not yet been generated |
| No matching captions | The error **lists the languages currently available for the video**. Adjust your language priority accordingly. Regional variants such as `en` and `en-US` are supported, so this message indicates a genuinely different language |
| Blocked by YouTube risk controls | The outbound IP is blocked; configure a proxy as described above |
| Age restricted | Videos requiring login cannot be retrieved. YouTube has deprecated the cookie-authentication route, so that route provides no workaround |
| Video unavailable | Invalid ID, deleted video, or private video |
| PO Token required | A newer verification mechanism used by some videos is not currently supported |

## How it works without an API key

The plugin uses the endpoints accessed by YouTube's web client: video page → player endpoint → caption endpoint. It does not use YouTube Data API v3.

The official API requires a key and has quotas, and `captions.download` **only downloads captions for your own channel**. It refuses other channels' videos, so it cannot provide this functionality.

The tradeoff is that these endpoints are undocumented and can change. Errors such as “InnerTube key not found” or “Failed to parse player response” usually mean YouTube changed them and the plugin needs an update.
