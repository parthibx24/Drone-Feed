# StarWatch detector (docs/09-ai.md). Build context: ai/detector
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    MODEL_VISIBLE=/app/models/visible.onnx MODEL_THERMAL=/app/models/thermal.onnx

WORKDIR /app
# A slow or stalling link to PyPI must not fail the build (seen on the
# Dokploy server 2026-11-24: numpy's download stalled past pip's 15 s read
# timeout). A newer pip resumes interrupted downloads; the cache mount keeps
# finished downloads for the next attempt without adding them to the image.
ENV PIP_DEFAULT_TIMEOUT=120 PIP_RETRIES=10 PIP_DISABLE_PIP_VERSION_CHECK=1
COPY requirements.txt .
RUN --mount=type=cache,target=/root/.cache/pip \
    pip install --upgrade "pip>=25.2" \
 && pip install --resume-retries 10 -r requirements.txt \
 && adduser --disabled-password --gecos "" --uid 10002 detector

COPY models/ models/
COPY detector/ detector/
COPY tests/ tests/
# Fail the build if the code or a model is broken.
RUN python -m unittest discover -s tests

USER detector
ENTRYPOINT ["python", "-m", "detector"]
