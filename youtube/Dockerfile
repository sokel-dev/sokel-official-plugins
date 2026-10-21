FROM python:3.12-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
 && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt "curl-cffi>=0.7"

COPY manifest.yml main.py sokel_gen.py ./
COPY docs ./docs

ENV PYTHONUNBUFFERED=1
CMD ["python", "main.py"]
