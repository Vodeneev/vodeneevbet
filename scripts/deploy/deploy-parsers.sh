#!/bin/bash
set -e

# Скрипт деплоя Parser Service на vm-parsers
# Использование: ./deploy-parsers.sh

VM_HOST="vm-parsers"
VM_USER="vodeneevm"
REMOTE_DIR="/home/vodeneevm/vodeneevbet"
SERVICE_NAME="vodeneevbet-parser"

echo "🚀 Deploying Parser Service to $VM_HOST"
echo "=========================================="

# Проверка подключения
echo "📡 Checking SSH connection..."
if ! ssh -o ConnectTimeout=5 "$VM_USER@$VM_HOST" "echo 'Connection OK'" 2>/dev/null; then
    echo "❌ Cannot connect to $VM_HOST. Please check SSH configuration."
    exit 1
fi

# Создание директорий на удаленной машине
echo "📁 Creating directories on remote machine..."
ssh "$VM_USER@$VM_HOST" "mkdir -p $REMOTE_DIR/{internal/parser,configs,keys,logs}"

# Синхронизация файлов
echo "📦 Syncing files..."
rsync -avz --delete \
    --exclude '.git' \
    --exclude '*.log' \
    --exclude '*.exe' \
    --exclude 'node_modules' \
    --exclude 'exports' \
    ./internal/parser/ "$VM_USER@$VM_HOST:$REMOTE_DIR/internal/parser/"
rsync -avz \
    ./internal/pkg/ "$VM_USER@$VM_HOST:$REMOTE_DIR/internal/pkg/"
rsync -avz \
    ./configs/ "$VM_USER@$VM_HOST:$REMOTE_DIR/configs/"
rsync -avz \
    ./go.mod "$VM_USER@$VM_HOST:$REMOTE_DIR/"
rsync -avz \
    ./go.sum "$VM_USER@$VM_HOST:$REMOTE_DIR/"

# Сборка на удаленной машине
echo "🔨 Building service on remote machine..."
ssh "$VM_USER@$VM_HOST" "cd $REMOTE_DIR && \
    export GOPATH=\$HOME/go && \
    export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin && \
    go mod download && \
    cd internal/parser && \
    go build -o parser -ldflags '-s -w' ."

# Копирование systemd unit файла
echo "⚙️  Installing systemd service..."
scp ./scripts/deploy/systemd/vodeneevbet-parser.service "$VM_USER@$VM_HOST:/tmp/"
ssh "$VM_USER@$VM_HOST" "sudo mv /tmp/vodeneevbet-parser.service /etc/systemd/system/ && \
    sudo sed -i 's|REMOTE_DIR|$REMOTE_DIR|g' /etc/systemd/system/vodeneevbet-parser.service && \
    sudo systemctl daemon-reload"

# Перезапуск сервиса
echo "🔄 Restarting service..."
ssh "$VM_USER@$VM_HOST" "sudo systemctl restart $SERVICE_NAME && \
    sudo systemctl enable $SERVICE_NAME"

# Проверка статуса
echo "✅ Checking service status..."
sleep 2
ssh "$VM_USER@$VM_HOST" "sudo systemctl status $SERVICE_NAME --no-pager -l"

echo ""
echo "✅ Deployment completed successfully!"
echo "📊 View logs: ssh $VM_USER@$VM_HOST 'sudo journalctl -u $SERVICE_NAME -f'"
