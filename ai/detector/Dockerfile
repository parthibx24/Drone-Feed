# StarWatch detector (docs/09-ai.md). Build context: ai/detector
FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 PYTHONUNBUFFERED=1 \
    MODEL_VISIBLE=/app/models/visible.onnx MODEL_THERMAL=/app/models/thermal.onnx

WORKDIR /app
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt \
 && adduser --disabled-password --gecos "" --uid 10002 detector

COPY models/ models/
COPY detector/ detector/
COPY tests/ tests/
# Fail the build if the code or a model is broken.
RUN python -m unittest discover -s tests

USER detector
ENTRYPOINT ["python", "-m", "detector"]
