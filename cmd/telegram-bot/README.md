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

**Important:** The bot is a long-running process. Once started, it will run continuously until stopped. For production, use systemd service or Docker with restart policy.

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

## Running as a Service

### Option 1: Systemd Service (Linux)

1. Copy the service file:
```bash
sudo cp deploy/vm-core/telegram-bot.service /etc/systemd/system/
```

2. Create override file for token:
```bash
sudo mkdir -p /etc/systemd/system/telegram-bot.service.d
sudo tee /etc/systemd/system/telegram-bot.service.d/override.conf <<EOF
[Service]
Environment="TELEGRAM_BOT_TOKEN=YOUR_BOT_TOKEN"
EOF
```

3. Reload and start:
```bash
sudo systemctl daemon-reload
sudo systemctl enable telegram-bot
sudo systemctl start telegram-bot
```

4. Check status:
```bash
sudo systemctl status telegram-bot
```

### Option 2: Docker

Build and run:
```bash
docker build -t telegram-bot -f cmd/telegram-bot/Dockerfile .
docker run -d \
  --name telegram-bot \
  --restart unless-stopped \
  -e TELEGRAM_BOT_TOKEN="YOUR_BOT_TOKEN" \
  -e CALCULATOR_URL="http://158.160.200.253" \
  telegram-bot
```

### Option 3: Docker Compose

Add to your docker-compose.yml:
```yaml
telegram-bot:
  build:
    context: .
    dockerfile: cmd/telegram-bot/Dockerfile
  restart: unless-stopped
  environment:
    - TELEGRAM_BOT_TOKEN=${TELEGRAM_BOT_TOKEN}
    - CALCULATOR_URL=http://158.160.200.253
```

## Security

- Use `-allowed-users` flag to restrict bot access to specific user IDs
- Keep your bot token secure (use environment variables, not command-line args in production)
- The bot connects to calculator service over HTTP - ensure proper network security
