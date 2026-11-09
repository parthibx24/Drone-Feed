# Build the React UI into /app/static
FROM node:22-slim AS frontend
WORKDIR /app/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM python:3.11-slim

# Prevent Python from writing .pyc files and enable unbuffered logging
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    VIEWER_HOST=0.0.0.0 \
    VIEWER_PORT=8091

WORKDIR /app

# Install curl for container health check and ffmpeg for server-side recording
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    ffmpeg \
 && rm -rf /var/lib/apt/lists/*

RUN mkdir -p /app/recording /app/recordings


# Install dependencies first for build caching
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Copy application source code
COPY . .
COPY --from=frontend /app/static ./static

# Expose default viewer port
EXPOSE 8091

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:${VIEWER_PORT:-8091}/api/config || exit 1

# Run application
CMD ["python", "__main__.py"]
