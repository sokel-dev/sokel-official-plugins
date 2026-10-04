# youtube-transcript

取 YouTube 字幕的第一方插件。取数思路参考 Python 的
[jdepoix/youtube-transcript-api](https://github.com/jdepoix/youtube-transcript-api)，
用 Go 重写并按本平台的契约重新切了操作边界。

用户向文档在 [docs/youtube-transcript.md](docs/youtube-transcript.md)（会被 embed 进二进制、
在插件详情页展示）。**这一份是给改代码的人看的。**

## 它是怎么取到字幕的

三步，全部走 YouTube 网页客户端自己在用的未公开接口：

```
① GET  youtube.com/watch?v=<id>              → 正则抠出 INNERTUBE_API_KEY
② POST youtube.com/youtubei/v1/player?key=…  → captions.playerCaptionsTracklistRenderer
③ GET  <captionTrack.baseUrl>                → timedtext XML，即字幕内容
```

翻译是给 ③ 的 URL 加 `&tlang=<code>`，由 YouTube 自己机翻。

**为什么不用 YouTube Data API v3**：要 key、有配额，且 `captions.download` 只能下自己频道的，
别人的视频一律 403 —— 官方 API 根本做不了这件事。

## 会过期的东西都在一处

`youtube.go` 顶部的常量是这个插件的全部脆弱面：

- `innertubeContext` —— 伪装成 `ANDROID` 客户端。网页客户端近年要 PO Token，Android 这条路暂时不要。
  **版本号会过期**，过期的症状是 player 接口开始要 PO Token。
- `apiKeyRe` / `consentRe` —— 从 HTML 里抠东西的正则。YouTube 改页面就会失效。
- `watchURL` / `innertubeURL`

改这些不需要动别处。反过来，收到「找不到 InnerTube key」「player 响应解析失败」的报障，
先看这里。

## 代码分工

| 文件 | 职责 | 能不能测 |
|---|---|---|
| `parse.go` | 纯函数：id 提取、XML 解析、选轨、拼文本 | **能**，`parse_test.go` 全覆盖 |
| `youtube.go` | HTTP + 风控识别 + 错误翻译 | 不能（要真网络） |
| `ops.go` | 三个操作的接线，刻意保持薄 | — |
| `schema/` | 契约声明，改完跑 `sokel-gen generate .` | — |

这么切是因为真正容易错的地方恰好都能脱网测：

- **id 形态**：用户是从地址栏直接粘的。只认 `watch?v=` 的话，手机分享的 `youtu.be`、
  Shorts、直播回放全会被拒，而他看着自己粘的明明是个好链接。
- **先去标签再解实体**：反过来的话，正文里字面写着 `&lt;b&gt;` 的内容会先变成 `<b>`
  再被当标签删掉，用户的原文就少了一截。测试里钉死了这条。
- **选轨顺序是「语言优先于类型」**：`zh-Hans,en` 的意思是中文比英文重要，
  有中文机翻时不该因为「英文有人工字幕」跳去英文。

## 改契约

```bash
sokel-gen generate .        # 或在本目录 go generate ./...
```

生成 `zz_types.go` / `zz_register.go` / `zz_credential.go`，别手改。

## 本地跑

```bash
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx go run .
```

## 已知取不到的

- **年龄限制视频**：需要登录。参考项目的 cookie 认证那条路已被 YouTube 改坏，
  所以这里**刻意不做** —— 留着只会让人以为能用。
- **要 PO Token 的视频**（URL 带 `&exp=xpe`）：识别出来明说，不让它退化成一句 XML 解析失败。
- **机房 IP**：会撞 429 或「确认你不是机器人」。唯一解法是凭证里配住宅代理，
  这一点必须在用户文档里说清楚，否则用户只会看到「被限流」然后以为插件坏了。
