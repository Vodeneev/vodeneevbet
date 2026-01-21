FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the bot
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o telegram-bot ./cmd/telegram-bot

FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /root/

COPY --from=builder /app/telegram-bot .

# Run the bot
CMD ["./telegram-bot"]
