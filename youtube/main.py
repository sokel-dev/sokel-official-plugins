"""YouTube 插件：取信息 / 下载 / 抓字幕。底层是 yt-dlp。

为什么是 Python：yt-dlp 本身就是 Python 库，`import yt_dlp` 直接拿结构化的 info dict。
Node 与 Go 那边的库（youtube-dl-exec / go-ytdlp）都是**去 spawn 这个 Python 程序**再解析
stdout——中间多一层进程与文本解析，错误信息还会退化成一行字符串。

运行：
    pip install -r requirements.txt
    SOKEL_ENDPOINT=http://localhost:8088 SOKEL_TOKEN=skp_xxx python main.py
"""

from __future__ import annotations

import asyncio
import logging
import os
import re
import shutil
import tempfile
from typing import Any, Dict, List, Optional

import yt_dlp

from sokel import Ctx
from sokel_gen import (
    DownloadAudioIn,
    DownloadAudioOut,
    DownloadIn,
    DownloadOut,
    Format,
    HealthCheckIn,
    HealthCheckOut,
    TranscriptIn,
    TranscriptOut,
    VideoInfoIn,
    VideoInfoOut,
    new_plugin,
    on_download,
    on_download_audio,
    on_health_check,
    on_transcript,
    on_video_info,
)

logging.basicConfig(level=logging.INFO, format="%(asctime)s %(message)s")

p = new_plugin()

MB = 1024 * 1024


def base_opts() -> Dict[str, Any]:
    """所有调用共用的 yt-dlp 选项。

    代理与 cookies 走**部署级环境变量**而不是凭证：平台一旦收到 credential_schema
    就会把插件标成需要凭证，于是下载一个公开视频也得先建一条凭证。
    """
    opts: Dict[str, Any] = {
        "quiet": True,
        "no_warnings": True,
        "noprogress": True,
        # 不让 yt-dlp 自己往 stderr 刷；出错走异常，由 SDK 翻成 error 帧
        "logger": logging.getLogger("yt_dlp"),
        "socket_timeout": 30,
        "retries": 3,
    }
    if proxy := os.environ.get("YTDLP_PROXY"):
        opts["proxy"] = proxy
    if cookies := os.environ.get("YTDLP_COOKIES"):
        opts["cookiefile"] = cookies
    return opts


def extract(url: str, extra: Optional[Dict[str, Any]] = None, download: bool = False) -> Dict[str, Any]:
    """跑一次 yt-dlp。**同步阻塞**，调用方一律用 asyncio.to_thread 包起来——
    直接 await 的话整个事件循环停在这儿，同副本的其它调用全被拖住。"""
    opts = base_opts()
    opts.update(extra or {})
    with yt_dlp.YoutubeDL(opts) as ydl:
        info = ydl.extract_info(url, download=download)
    # 播放列表：只取第一条，别让「一个链接下出 200 个文件」成为默认行为
    if info.get("_type") == "playlist" and info.get("entries"):
        info = info["entries"][0]
    return info


# —— 取信息 ——


async def video_info(ctx: Ctx, in_: VideoInfoIn) -> VideoInfoOut:
    info = await asyncio.to_thread(extract, in_.url)
    formats: List[Format] = []
    for f in info.get("formats") or []:
        if not f.get("height"):  # 纯音频轨不进清晰度表
            continue
        size = f.get("filesize") or f.get("filesize_approx") or 0
        formats.append(
            Format(
                format_id=str(f.get("format_id") or ""),
                ext=str(f.get("ext") or ""),
                height=int(f.get("height") or 0),
                fps=int(f.get("fps") or 0),
                filesize_mb=round(size / MB, 1) if size else 0.0,
            )
        )
    # 同一高度留一条（上游同高度常有 5 种编码），高的在前
    seen, uniq = set(), []
    for f in sorted(formats, key=lambda x: -x.height):
        if f.height in seen:
            continue
        seen.add(f.height)
        uniq.append(f)
    date = str(info.get("upload_date") or "")
    return VideoInfoOut(
        id=str(info.get("id") or ""),
        title=str(info.get("title") or ""),
        uploader=str(info.get("uploader") or ""),
        upload_date=f"{date[:4]}-{date[4:6]}-{date[6:]}" if len(date) == 8 else "",
        duration_seconds=int(info.get("duration") or 0),
        view_count=int(info.get("view_count") or 0),
        description=str(info.get("description") or ""),
        thumbnail=str(info.get("thumbnail") or ""),
        webpage_url=str(info.get("webpage_url") or in_.url),
        is_live=bool(info.get("is_live")),
        subtitle_langs=sorted({*(info.get("subtitles") or {}), *(info.get("automatic_captions") or {})}),
        formats=uniq,
    )


on_video_info(p, video_info)


# —— 下载 ——


def format_selector(quality: str, format_id: str = "") -> Dict[str, Any]:
    # 点名某个 format_id：下载站上那份「质量列表」就是这么用的——先列上游已有的流，
    # 再让人点一个。+bestaudio 是因为高清那些流本身**只有视频轨**。
    if format_id:
        return {"format": f"{format_id}+bestaudio/{format_id}", "merge_output_format": "mp4"}
    if quality == "best":
        return {"format": "bestvideo+bestaudio/best", "merge_output_format": "mp4"}
    # 高度上限：拿不到该高度时回落到更低的，而不是直接失败
    return {"format": f"bestvideo[height<={quality}]+bestaudio/best[height<={quality}]/best",
            "merge_output_format": "mp4"}


def probed_size(info: Dict[str, Any]) -> int:
    """预估体积。

    合并格式（视频轨 + 音频轨）的顶层常常没有 filesize，只有 requested_formats 里
    各轨一份——只看顶层的话，这道体积闸在**最常见的那种下载**上就是个空转。
    """
    top = info.get("filesize") or info.get("filesize_approx") or 0
    tracks = sum(
        (f.get("filesize") or f.get("filesize_approx") or 0)
        for f in (info.get("requested_formats") or [])
    )
    return max(int(top), int(tracks))


async def download(ctx: Ctx, in_: DownloadIn) -> DownloadOut:
    # 0 = 不限。平台侧的分块上传没有体积上限，这道闸是插件自己设的护栏，
    # 真需要整部片子时应该能关掉——但默认开着，理由见契约里那段 desc。
    limit = max(in_.max_filesize_mb, 0) * MB
    tmp = tempfile.mkdtemp(prefix="yt-")
    try:
        # 先探一次体积：超限就**不下载**直接报错。下到一半再失败的话，
        # 用户看到的是一句网络错误，而真正的原因是「这视频太大」。
        sel = format_selector(in_.quality, in_.format_id)
        probe = await asyncio.to_thread(extract, in_.url, sel)
        approx = probed_size(probe)
        if limit and approx and approx > limit:
            raise ValueError(
                f"视频约 {approx / MB:.0f}MB，超过上限 {in_.max_filesize_mb}MB —— "
                f"调高上限、或换更低清晰度、或用「仅音频」"
            )
        opts = dict(sel)
        opts["outtmpl"] = os.path.join(tmp, "%(title).150B.%(ext)s")
        if limit:
            opts["max_filesize"] = limit  # 兜底：探不准时由 yt-dlp 中途掐断
        info = await asyncio.to_thread(extract, in_.url, opts, True)

        files = [os.path.join(tmp, f) for f in os.listdir(tmp)]
        if not files:
            raise RuntimeError("yt-dlp 没有产出文件（多半是体积超限被中途掐断）")
        path = max(files, key=os.path.getsize)
        size = os.path.getsize(path)
        if limit and size > limit:
            raise ValueError(f"下载完是 {size / MB:.0f}MB，超过上限 {in_.max_filesize_mb}MB")

        ext = os.path.splitext(path)[1].lstrip(".")
        name = f"{in_.filename}.{ext}" if in_.filename else os.path.basename(path)
        # 边读边传：几百 MB 的视频不能整个读进内存（那是容器被 OOM 杀掉的经典原因）
        ref = await ctx.upload_file(path, name=name)
        return DownloadOut(
            file=ref,
            title=str(info.get("title") or ""),
            duration_seconds=int(info.get("duration") or 0),
            filesize_mb=round(size / MB, 1),
            ext=ext,
            # 两条流合并过 = 上游没有这个清晰度的单文件版。只是换容器不重编码，
            # 但它解释了「为什么 1080p 比 360p 多花几秒」。
            merged=len(info.get("requested_formats") or []) > 1,
            webpage_url=str(info.get("webpage_url") or in_.url),
        )
    finally:
        shutil.rmtree(tmp, ignore_errors=True)  # 失败也要清，否则容器磁盘迟早满


on_download(p, download)


# —— 音频 ——


async def download_audio(ctx: Ctx, in_: DownloadAudioIn) -> DownloadAudioOut:
    limit = max(in_.max_filesize_mb, 0) * MB  # 0 = 不限
    tmp = tempfile.mkdtemp(prefix="yt-audio-")
    try:
        opts: Dict[str, Any] = {"format": "bestaudio/best", "outtmpl": os.path.join(tmp, "%(title).150B.%(ext)s")}
        probe = await asyncio.to_thread(extract, in_.url, opts)
        src_ext = str(probe.get("ext") or "")
        src_codec = str(probe.get("acodec") or "")
        approx = probed_size(probe)
        if limit and approx and approx > limit:
            raise ValueError(f"音频约 {approx / MB:.0f}MB，超过上限 {in_.max_filesize_mb}MB")

        # 转码是**真的后期处理**：ffmpeg 重新编码一遍，慢且有损。
        # 所以默认 source（原样），只有明确要某种格式时才转——而且如果上游本来就是
        # 那个格式，也不必转（这里的判断让 transcoded 这个出参说的是实话）。
        transcode = in_.audio_format != "source" and in_.audio_format != src_ext
        if transcode:
            opts["postprocessors"] = [{
                "key": "FFmpegExtractAudio",
                "preferredcodec": in_.audio_format,
                "preferredquality": in_.bitrate,
            }]
        info = await asyncio.to_thread(extract, in_.url, opts, True)

        files = [os.path.join(tmp, f) for f in os.listdir(tmp)]
        if not files:
            raise RuntimeError("yt-dlp 没有产出文件")
        path = max(files, key=os.path.getsize)
        size = os.path.getsize(path)
        if limit and size > limit:
            raise ValueError(f"下载完是 {size / MB:.0f}MB，超过上限 {in_.max_filesize_mb}MB")

        ext = os.path.splitext(path)[1].lstrip(".")
        name = f"{in_.filename}.{ext}" if in_.filename else os.path.basename(path)
        ref = await ctx.upload_file(path, name=name)
        return DownloadAudioOut(
            file=ref,
            title=str(info.get("title") or ""),
            duration_seconds=int(info.get("duration") or 0),
            filesize_mb=round(size / MB, 1),
            ext=ext,
            codec=in_.audio_format if transcode else src_codec,
            transcoded=transcode,
            webpage_url=str(info.get("webpage_url") or in_.url),
        )
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


on_download_audio(p, download_audio)


# —— 字幕 ——


def pick_subtitle(info: Dict[str, Any], want: str) -> tuple[str, bool]:
    """选一条字幕：优先人工、其次自动；语言按 BCP-47 前缀匹配，都没有就回落第一条。"""
    manual = info.get("subtitles") or {}
    auto = info.get("automatic_captions") or {}
    want = (want or "").lower()
    for table, automatic in ((manual, False), (auto, True)):
        for lang in table:
            if lang.lower() == want:
                return lang, automatic
        for lang in table:  # zh 匹配 zh-Hans / zh-CN
            if want and (lang.lower().startswith(want) or want.startswith(lang.lower())):
                return lang, automatic
    for table, automatic in ((manual, False), (auto, True)):
        if table:
            return next(iter(table)), automatic
    raise ValueError("这个视频没有任何字幕（人工与自动都没有）")


def vtt_to_text(vtt: str) -> str:
    """VTT → 纯文本：去时间轴、去内联标签、去连续重复行。

    自动字幕是**滚动**的：相邻两条会重复上一条的尾巴，直接拼起来能重复三四遍。
    """
    out: List[str] = []
    for raw in vtt.splitlines():
        line = raw.strip()
        if not line or "-->" in line or line.startswith(("WEBVTT", "Kind:", "Language:", "NOTE")):
            continue
        if line.isdigit():
            continue
        line = re.sub(r"<[^>]+>", "", line).strip()
        if line and (not out or out[-1] != line):
            out.append(line)
    return "\n".join(out)


async def transcript(ctx: Ctx, in_: TranscriptIn) -> TranscriptOut:
    tmp = tempfile.mkdtemp(prefix="yt-sub-")
    try:
        info = await asyncio.to_thread(extract, in_.url)
        lang, automatic = pick_subtitle(info, in_.lang)
        opts = {
            "skip_download": True,  # 不要视频本身：这正是这个操作比「下载→转写」快一个数量级的原因
            "writesubtitles": not automatic,
            "writeautomaticsub": automatic,
            "subtitleslangs": [lang],
            "subtitlesformat": "vtt",
            "outtmpl": os.path.join(tmp, "sub.%(ext)s"),
        }
        await asyncio.to_thread(extract, in_.url, opts, True)
        subs = [f for f in os.listdir(tmp) if f.endswith(".vtt")]
        if not subs:
            raise RuntimeError(f"字幕语言 {lang} 拉不下来（上游可能刚下架了它）")
        with open(os.path.join(tmp, subs[0]), encoding="utf-8") as fh:
            text = vtt_to_text(fh.read())
        return TranscriptOut(
            text=text,
            lang_used=lang,
            automatic=automatic,
            title=str(info.get("title") or ""),
            char_count=len(text),
        )
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


on_transcript(p, transcript)


# —— 体检 ——


def probe_network() -> None:
    """能不能连上 youtube.com。走一次最轻的 HTTP 请求，不解析任何视频。"""
    import urllib.request

    proxy = os.environ.get("YTDLP_PROXY")
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({"http": proxy, "https": proxy} if proxy else {})
    )
    req = urllib.request.Request("https://www.youtube.com/robots.txt", method="GET")
    with opener.open(req, timeout=15) as resp:
        if resp.status >= 400:
            raise RuntimeError(f"HTTP {resp.status}")


async def health_check(ctx: Ctx, _in: HealthCheckIn) -> HealthCheckOut:
    # 不可用要返回 ok=false 而不是抛错：抛错的话平台只能说「调用失败」，
    # 说不出是缺 ffmpeg 还是出不了网，而这两件事的处理办法完全不同。
    if shutil.which("ffmpeg") is None:
        return HealthCheckOut(ok=False, message="缺 ffmpeg —— 高清视频需要它把视频轨与音频轨合并")
    try:
        # 探的是**出网**，不是某个视频。第一版这里钉了一个具体的视频 id 当探针，
        # 而那个视频后来下架了——体检于是永远报「连不上」，真实原因却是「探针没了」。
        # 任何「拿某条外部内容当健康信号」的写法都有这个毛病：内容会消失，链路不会。
        await asyncio.to_thread(probe_network)
    except Exception as e:  # noqa: BLE001
        proxy = os.environ.get("YTDLP_PROXY") or "(未配)"
        return HealthCheckOut(ok=False, message=f"连不上 YouTube（代理 {proxy}）：{e}")
    return HealthCheckOut(ok=True, message=f"yt-dlp {yt_dlp.version.__version__}，ffmpeg 就位，出网正常")


on_health_check(p, health_check)


if __name__ == "__main__":
    asyncio.run(p.run())
