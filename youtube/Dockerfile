FROM python:3.12-slim

COPY --from=denoland/deno:bin-2.9.7 /deno /usr/local/bin/deno

RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt "curl-cffi>=0.7"
RUN deno eval 'if (1 + 1 !== 2) throw new Error("runtime unavailable")' \
 && python -c 'import yt_dlp_ejs; from yt_dlp import YoutubeDL; assert YoutubeDL().params["js_runtimes"].get("deno") is not None'

COPY manifest.yml main.py sokel_gen.py ./
COPY docs ./docs

ENV PYTHONUNBUFFERED=1
CMD ["python", "main.py"]
