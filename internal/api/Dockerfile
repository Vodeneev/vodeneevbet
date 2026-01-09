# syntax=docker/dockerfile:1.7

FROM golang:1.24.7-bookworm AS builder

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

COPY go.mod go.sum ./
RUN go mod download

COPY internal ./internal

RUN go build -trimpath -ldflags="-s -w" -o /out/api ./internal/api

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/api /app/api
COPY --from=builder /src/internal/api/static /app/static

EXPOSE 8081

ENTRYPOINT ["/app/api"]
