# StarWatch central server: drone-gateway (Go) serving the React UI.
#   docker build -t starwatch-server .

# 1. React UI
FROM node:22-slim AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 2. Go server
FROM golang:1.26-alpine AS server
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY internal/ internal/
COPY server/ server/
COPY drone/ drone/
COPY tools/ tools/
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /out/drone-gateway ./server/cmd/drone-gateway

# 3. Runtime: small image with ffmpeg/ffprobe for server-side recording
FROM alpine:3.22
RUN apk add --no-cache ffmpeg ca-certificates tzdata wget \
 && adduser -D -H -u 10001 starwatch \
 && mkdir -p /recordings /config \
 && chown starwatch /recordings
COPY --from=server /out/drone-gateway /usr/local/bin/drone-gateway
COPY --from=web /src/frontend/dist /app/web
USER starwatch
EXPOSE 8091
VOLUME ["/recordings"]
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -q -O /dev/null http://127.0.0.1:8091/healthz || exit 1
ENTRYPOINT ["drone-gateway", "-config", "/config/gateway.yaml", "-json-logs"]
