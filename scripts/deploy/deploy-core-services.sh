#!/bin/bash
set -e

# Скрипт деплоя Calculator и API Services на vm-core-services
# Использование: ./deploy-core-services.sh

VM_HOST="vm-core-services"
VM_USER="vodeneevm"
REMOTE_DIR="/home/vodeneevm/vodeneevbet"
CALCULATOR_SERVICE="vodeneevbet-calculator"
API_SERVICE="vodeneevbet-api"

echo "🚀 Deploying Core Services to $VM_HOST"
echo "======================================="

# Проверка подключения
echo "📡 Checking SSH connection..."
if ! ssh -o ConnectTimeout=5 "$VM_USER@$VM_HOST" "echo 'Connection OK'" 2>/dev/null; then
    echo "❌ Cannot connect to $VM_HOST. Please check SSH configuration."
    exit 1
fi

# Создание директорий на удаленной машине
echo "📁 Creating directories on remote machine..."
ssh "$VM_USER@$VM_HOST" "mkdir -p $REMOTE_DIR/{internal/{calculator,api},configs,keys,logs,static}"

# Синхронизация файлов
echo "📦 Syncing files..."
rsync -avz --delete \
    --exclude '.git' \
    --exclude '*.log' \
    --exclude '*.exe' \
    --exclude 'node_modules' \
    --exclude 'exports' \
    ./internal/calculator/ "$VM_USER@$VM_HOST:$REMOTE_DIR/internal/calculator/"
rsync -avz --delete \
    --exclude '.git' \
    --exclude '*.log' \
    --exclude '*.exe' \
    --exclude 'node_modules' \
    ./internal/api/ "$VM_USER@$VM_HOST:$REMOTE_DIR/internal/api/"
rsync -avz \
    ./internal/pkg/ "$VM_USER@$VM_HOST:$REMOTE_DIR/internal/pkg/"
rsync -avz \
    ./configs/ "$VM_USER@$VM_HOST:$REMOTE_DIR/configs/"
rsync -avz \
    ./go.mod "$VM_USER@$VM_HOST:$REMOTE_DIR/"
rsync -avz \
    ./go.sum "$VM_USER@$VM_HOST:$REMOTE_DIR/"

# Сборка Calculator
echo "🔨 Building Calculator service..."
ssh "$VM_USER@$VM_HOST" "cd $REMOTE_DIR && \
    export GOPATH=\$HOME/go && \
    export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin && \
    go mod download && \
    cd internal/calculator && \
    go build -o calculator -ldflags '-s -w' ."

# Сборка API
echo "🔨 Building API service..."
ssh "$VM_USER@$VM_HOST" "cd $REMOTE_DIR && \
    export GOPATH=\$HOME/go && \
    export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin && \
    cd internal/api && \
    go build -o api -ldflags '-s -w' ."

# Копирование systemd unit файлов
echo "⚙️  Installing systemd services..."
scp ./scripts/deploy/systemd/vodeneevbet-calculator.service "$VM_USER@$VM_HOST:/tmp/"
scp ./scripts/deploy/systemd/vodeneevbet-api.service "$VM_USER@$VM_HOST:/tmp/"

ssh "$VM_USER@$VM_HOST" "sudo mv /tmp/vodeneevbet-calculator.service /etc/systemd/system/ && \
    sudo mv /tmp/vodeneevbet-api.service /etc/systemd/system/ && \
    sudo sed -i 's|REMOTE_DIR|$REMOTE_DIR|g' /etc/systemd/system/vodeneevbet-calculator.service && \
    sudo sed -i 's|REMOTE_DIR|$REMOTE_DIR|g' /etc/systemd/system/vodeneevbet-api.service && \
    sudo systemctl daemon-reload"

# Перезапуск сервисов
echo "🔄 Restarting services..."
ssh "$VM_USER@$VM_HOST" "sudo systemctl restart $CALCULATOR_SERVICE && \
    sudo systemctl enable $CALCULATOR_SERVICE && \
    sudo systemctl restart $API_SERVICE && \
    sudo systemctl enable $API_SERVICE"

# Проверка статуса
echo "✅ Checking service status..."
sleep 2
echo ""
echo "--- Calculator Status ---"
ssh "$VM_USER@$VM_HOST" "sudo systemctl status $CALCULATOR_SERVICE --no-pager -l | head -10"
echo ""
echo "--- API Status ---"
ssh "$VM_USER@$VM_HOST" "sudo systemctl status $API_SERVICE --no-pager -l | head -10"

echo ""
echo "✅ Deployment completed successfully!"
echo "📊 View Calculator logs: ssh $VM_USER@$VM_HOST 'sudo journalctl -u $CALCULATOR_SERVICE -f'"
echo "📊 View API logs: ssh $VM_USER@$VM_HOST 'sudo journalctl -u $API_SERVICE -f'"
