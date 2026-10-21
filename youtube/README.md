# youtube

> 给**改代码的人**看的。给用户看的在 [`docs/youtube.md`](docs/youtube.md)。

基于 [yt-dlp](https://github.com/yt-dlp/yt-dlp) 的下载插件：取信息 / 下载 / 抓字幕。

## 为什么是 Python

yt-dlp **本身就是 Python 库**：`import yt_dlp` 直接拿到结构化的 info dict（格式表、字幕表、
体积预估都在里面）。Node 与 Go 那边的库（youtube-dl-exec / go-ytdlp）都是去 spawn 这个
Python 程序再解析 stdout —— 中间多一层进程与文本解析，错误信息还会退化成一行字符串。

纯 Go 的实现（kkdai/youtube 一类）不依赖 yt-dlp，但 YouTube 每隔一阵就改播放器的签名逻辑，
那类库跟不上，表现是「某天开始所有视频都下不了」。yt-dlp 几乎每周一版，跟得上的是它。

## 结构

| 文件 | 作用 |
|---|---|
| `manifest.yml` | 契约声明。**改这里**，然后 `sokel-gen generate .` |
| `sokel_gen.py` | 生成物（pydantic 模型 + 注册口）。别手改 |
| `main.py` | 四个操作的实现 |
| `Dockerfile` | python + ffmpeg + yt-dlp |

## 开发

```bash
uv venv && uv pip install -r requirements.txt
sokel-gen generate .          # 改了 manifest.yml 就重新生成
YTDLP_PROXY=http://127.0.0.1:7897 \
SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx python main.py
```

## 几个刻意的取舍

- **可选凭证**：`cookies` 与 `proxy` 都声明为可选密文，沿用平台的可选凭证路由。
  从 SDK 的 typed credential 读取；非空字段优先于部署环境变量。cookies 只写入每次提取的
  `0600` 临时文件，yt-dlp 退出后清理；不修改环境变量、不缓存跨调用会话。
- **默认 720p / 512MB，且超限时不下载；但 `0` = 不限**。平台侧分块上传没有体积上限
  （`storage/files.go` 只累加 up.size），所以这道闸是插件自己的护栏，真要整部片子时能关掉。
  先探体积再决定，比下到一半失败可读得多（用户看到的会是一句网络错误，真实原因却是体积）。
- **「清晰度」不是转码**。YouTube 本来就存着多条流，选清晰度 = 挑一条已有的流。
  本地只有两件事是真处理：**合并**（remux，不重编码，出参 `merged`）与**转码**
  （只在音频指定 mp3/m4a/opus 时，出参 `transcoded`）。这两个出参存在的意义就是
  让「这次到底动没动过原始数据」在数据里可见，而不是靠人猜。
  注意合并格式的顶层常常没有 `filesize`，只有 `requested_formats` 里各轨一份——
  只看顶层的话这道闸在最常见的下载上是空转（`probed_size` 就是为此）。
- **体检探的是出网，不是某个视频**。第一版钉了一个具体 video id 当探针，而那个视频后来
  下架了，体检于是永远报「连不上」。任何「拿某条外部内容当健康信号」的写法都有这个毛病。
- **`yt_dlp` 是同步阻塞的**，一律 `asyncio.to_thread` 包起来。直接 await 会让整个事件循环
  停在下载上，同副本的其它调用全被拖住。
- **requirements 不钉死 yt-dlp 版本**，并且要定期重建镜像。钉死的那天起，这个插件就在等
  上游某次改签名之后突然全线失败。

## 实测

Python 3.12 + yt-dlp 2026.08.19 + ffmpeg 8.0，经代理：
取信息 / 仅音频下载 / 抓字幕 / 体积闸（689MB 的片子在 2MB 上限下被正确拒绝）四条都通。

## Official distribution

Version 0.2.0 updates the SDK to v0.7.6 and regenerates the contract from `manifest.yml`.
The four operation IDs and their input/output fields are unchanged. The public export retains
this plugin's Python Dockerfile; CI runs `python -m unittest discover -v` and `sokel-gen check .`.

Release the public mirror with tag `youtube/v0.2.0`. Its release workflow builds the image,
then opens the first `sokel/youtube` catalog entry with that image's digest, the usage document,
and `locales/en.json`. Merge the reviewed catalog PR before updating the platform's `registry.lock`.
The separate `youtube-transcript` plugin remains available.

## Verification for Issue #226

Test-first results from the source repository:

```bash
python3 -m unittest discover -s scripts -p 'test_*.py' -v
# RED: Python youtube was omitted from the public export; GREEN: 1 test passed.
python3 -m unittest discover -s public/scripts -p 'test_*.py' -v
# RED: Python omitted from the image matrix; first release refused without an entry.
# GREEN: 2 tests passed, including both runtimes and subsequent version updates.
```

The public mirror runs the second command with `-s scripts`. The nine plugin tests
(`cd youtube && python -m unittest discover -v`) also pass inside the built container with
network access disabled. Local coverage of `main.py` is 93%; changed executable lines in
`plugin-matrix.py` and `registry-entry.py` are covered at 100% and 83%, respectively.
Uncovered paths include live network probing and some first-release error branches.

`sokel-gen@v0.7.6 check` passes. All five operation contracts (including health_check)
match the previous generated contract. The catalog's `go test -race ./...` and
`go run ./cmd/build-index -base <baseline> -site <output> .` pass with an isolated
first-release fixture. Its synthetic digest is test data only, never a publishable image reference.
Live platform registration and downloads against the external service were not exercised.

## Optional credentials and channel releases (0.3.0)

`test_credentials.py` exercises SDK dispatch, all four media operations, concurrent cookie isolation,
format validation, secret-safe health errors and anonymous access. Run `python -m unittest discover -v`.
The existing secret form supports multiline paste; use `secret`, not the JSON-specific `secret-file` control.
Cookie files must remain writable until yt-dlp closes. The health probe checks connectivity and syntax,
not account validity; use a real target video to verify the session after configuring credentials.

`channel_latest` accepts a channel URL or handle plus a bounded limit and content tab. It preserves
playlist results rather than the single-video helper's first-entry behavior. `test_channel.py`
covers metadata-only extraction, credentials, order, empty/unavailable entries, and invalid input.
Publication 0.3.0 adds this operation and optional credentials without changing existing media I/O.

Version 0.3.1 fixes release packaging: later releases refresh the catalog usage document and retire
translations for removed source strings. The v0.3.0 image built successfully but catalog admission
correctly blocked the stale documentation bundle. The public release-tool regression reproduces
that failure before the fix, and the real catalog admission passes with the rebuilt bundle.
