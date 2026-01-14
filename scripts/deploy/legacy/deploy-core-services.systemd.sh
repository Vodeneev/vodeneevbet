#!/bin/bash
set -e

# Legacy systemd-based deploy script (vm-core-services).
# Kept for reference; prefer docker compose deploy in ../deploy-core-services.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../../.." && pwd)"
cd "$PROJECT_ROOT"

VM_HOST="${VM_HOST:-158.160.200.253}"
VM_USER="${VM_USER:-vodeneevm}"
REMOTE_DIR="/home/vodeneevm/vodeneevbet"
CALCULATOR_SERVICE="vodeneevbet-calculator"
API_SERVICE="vodeneevbet-api"

echo "🚀 [legacy] Deploying Core Services (systemd) to $VM_HOST"

echo "📡 Checking SSH connection..."
if ! ssh -o ConnectTimeout=5 -o StrictHostKeyChecking=no "$VM_USER@$VM_HOST" "echo 'Connection OK'" 2>/dev/null; then
    echo "❌ Cannot connect to $VM_HOST. Please check SSH configuration."
    exit 1
fi

echo "📁 Creating directories on remote machine..."
ssh "$VM_USER@$VM_HOST" "mkdir -p $REMOTE_DIR/{internal/{calculator,api},configs,keys,logs,static}"

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

echo "🔨 Building Calculator service..."
ssh "$VM_USER@$VM_HOST" "cd $REMOTE_DIR && \
    export GOPATH=\$HOME/go && \
    export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin && \
    go mod download && \
    cd internal/calculator && \
    go build -o calculator -ldflags '-s -w' ."

echo "🔨 Building API service..."
ssh "$VM_USER@$VM_HOST" "cd $REMOTE_DIR && \
    export GOPATH=\$HOME/go && \
    export PATH=\$PATH:/usr/local/go/bin:\$GOPATH/bin && \
    cd internal/api && \
    go build -o api -ldflags '-s -w' ."

echo "⚙️  Installing systemd services..."
scp "$PROJECT_ROOT/scripts/deploy/legacy/systemd/vodeneevbet-calculator.service" "$VM_USER@$VM_HOST:/tmp/"
scp "$PROJECT_ROOT/scripts/deploy/legacy/systemd/vodeneevbet-api.service" "$VM_USER@$VM_HOST:/tmp/"

ssh "$VM_USER@$VM_HOST" "sudo mv /tmp/vodeneevbet-calculator.service /etc/systemd/system/ && \
    sudo mv /tmp/vodeneevbet-api.service /etc/systemd/system/ && \
    sudo sed -i 's|REMOTE_DIR|$REMOTE_DIR|g' /etc/systemd/system/vodeneevbet-calculator.service && \
    sudo sed -i 's|REMOTE_DIR|$REMOTE_DIR|g' /etc/systemd/system/vodeneevbet-api.service && \
    sudo systemctl daemon-reload"

echo "🔄 Restarting services..."
ssh "$VM_USER@$VM_HOST" "sudo systemctl restart $CALCULATOR_SERVICE && \
    sudo systemctl enable $CALCULATOR_SERVICE && \
    sudo systemctl restart $API_SERVICE && \
    sudo systemctl enable $API_SERVICE"

echo "✅ [legacy] Deployment completed successfully!"

