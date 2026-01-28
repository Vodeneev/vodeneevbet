# syntax=docker/dockerfile:1.7

FROM golang:1.24.7-bookworm AS builder

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY internal ./internal

COPY cmd ./cmd

RUN go build -trimpath -ldflags="-s -w" -o /out/parser ./cmd/parser

# Use debian-slim instead of distroless to support Chrome/Chromium
FROM debian:bookworm-slim

WORKDIR /app

# Install Chrome dependencies and Chrome
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    wget \
    gnupg \
    libnss3 \
    libatk-bridge2.0-0 \
    libdrm2 \
    libxkbcommon0 \
    libxcomposite1 \
    libxdamage1 \
    libxfixes3 \
    libxrandr2 \
    libgbm1 \
    libasound2 \
    && mkdir -p /etc/apt/keyrings && \
    wget -q -O - https://dl-ssl.google.com/linux/linux_signing_key.pub | gpg --dearmor -o /etc/apt/keyrings/google-chrome.gpg && \
    echo "deb [arch=amd64 signed-by=/etc/apt/keyrings/google-chrome.gpg] http://dl.google.com/linux/chrome/deb/ stable main" > /etc/apt/sources.list.d/google-chrome.list && \
    apt-get update && \
    apt-get install -y --no-install-recommends \
    google-chrome-stable \
    && ln -s /usr/bin/google-chrome-stable /usr/bin/google-chrome \
    && rm -rf /var/lib/apt/lists/*

# Create non-root user
RUN useradd -m -u 1000 appuser && \
    chown -R appuser:appuser /app

COPY --from=builder /out/parser /app/parser

USER appuser

ENTRYPOINT ["/app/parser"]
