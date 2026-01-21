# Telegram Bot for Value Bet Calculator

Telegram bot wrapper for the `/diffs/top` endpoint with different parameters.

## Features

- Get top value bet differences
- Filter by match status (live/upcoming)
- Adjustable limit (1-50)
- English interface

## Setup

1. Create a Telegram bot via [@BotFather](https://t.me/botfather)
2. Get your bot token
3. Set environment variables or use command-line flags

## Usage

### Command Line

```bash
./telegram-bot \
  -token "YOUR_BOT_TOKEN" \
  -calculator-url "http://158.160.200.253" \
  -allowed-users "123456789,987654321"  # Optional: restrict access
```

### Environment Variables

```bash
export TELEGRAM_BOT_TOKEN="YOUR_BOT_TOKEN"
export CALCULATOR_URL="http://158.160.200.253"
./telegram-bot
```

## Commands

- `/start` or `/help` - Show help message
- `/top [limit]` - Get top value bet differences (default: 5)
- `/live [limit]` - Get top differences for live matches (default: 5)
- `/upcoming [limit]` - Get top differences for upcoming matches (default: 5)

## Examples

```
/top 10        # Get top 10 differences
/live 5        # Get top 5 live matches
/upcoming 3    # Get top 3 upcoming matches
```

You can also send plain text messages:
```
top 10
live 5
upcoming 3
```

## Build

```bash
go build -o telegram-bot ./cmd/telegram-bot
```

## Docker

Example Dockerfile:

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o telegram-bot ./cmd/telegram-bot

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/telegram-bot .
CMD ["./telegram-bot"]
```

## Security

- Use `-allowed-users` flag to restrict bot access to specific user IDs
- Keep your bot token secure (use environment variables, not command-line args in production)
- The bot connects to calculator service over HTTP - ensure proper network security
