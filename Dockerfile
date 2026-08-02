# syntax=docker/dockerfile:1.7

FROM node:22-alpine AS web-build
WORKDIR /src/web/app
COPY web/app/package.json web/app/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/app/ ./
RUN npm run build

FROM golang:1.25-alpine AS go-build
RUN apk add --no-cache ca-certificates git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vod-web ./cmd/vod-web && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vod-worker ./cmd/vod-worker && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vod-outbox-relay ./cmd/vod-outbox-relay && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vod-clickhouse-sink ./cmd/vod-clickhouse-sink

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates ffmpeg tesseract-ocr tesseract-ocr-data-eng tzdata && \
    addgroup -g 10001 -S vodcoach && \
    adduser -u 10001 -S -D -H -G vodcoach vodcoach && \
    mkdir -p /app/bin /app/data/raw/youtube /app/data/raw/uploads /app/data/processed && \
    chown -R vodcoach:vodcoach /app
WORKDIR /app
COPY --from=go-build /out/ /app/bin/
COPY --from=web-build /src/web/app/dist/ /app/web/app/dist/
COPY --chown=vodcoach:vodcoach data/manifests/ /app/data/manifests/
COPY --chown=vodcoach:vodcoach deployments/migrations/ /app/deployments/migrations/
USER 10001:10001
ENV APP_ENV=container \
    LOG_FORMAT=json \
    PATH=/app/bin:$PATH
EXPOSE 8090
VOLUME ["/app/data/raw", "/app/data/processed"]
CMD ["/app/bin/vod-web", "--addr=:8090", "--static-dir=/app/web/app/dist"]
