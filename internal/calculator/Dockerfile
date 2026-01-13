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

RUN go build -trimpath -ldflags="-s -w" -o /out/calculator ./cmd/calculator

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

COPY --from=builder /out/calculator /app/calculator

ENTRYPOINT ["/app/calculator"]
