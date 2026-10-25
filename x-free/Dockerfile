FROM python:3.12-slim

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py list_members.py sokel_gen.py ./
COPY docs ./docs

ENV PYTHONUNBUFFERED=1
CMD ["python", "main.py"]
