#!/bin/bash

echo "🚀 Starting Value Bet Finder System"
echo "=================================="

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "❌ Please run this script from the project root directory"
    exit 1
fi

# Create logs directory
mkdir -p logs

echo "📦 Building dependencies..."
go mod tidy

echo "🔧 Starting infrastructure..."
docker-compose up -d

echo "⏳ Waiting for services to start..."
sleep 5

echo "📊 Exporting current data..."
./export_data.sh

echo "📊 Starting parser..."
go run ./cmd/parser -config configs/production.yaml > logs/parser.log 2>&1 &
PARSER_PID=$!

echo "🧮 Starting calculator..."
go run ./cmd/calculator -config configs/production.yaml > logs/calculator.log 2>&1 &
CALCULATOR_PID=$!

echo ""
echo "✅ System started successfully!"
echo ""
echo "📝 Logs:"
echo "   - Parser: logs/parser.log"
echo "   - Calculator: logs/calculator.log"
echo ""
echo "🛑 To stop the system:"
echo "   - Press Ctrl+C"
echo "   - Or run: pkill -f 'go run ./cmd/' && docker-compose down"
echo ""

# Wait for user interrupt
trap 'echo ""; echo "🛑 Stopping system..."; kill $PARSER_PID $CALCULATOR_PID 2>/dev/null; docker-compose down; echo "✅ System stopped"; exit 0' INT

# Keep script running
wait
