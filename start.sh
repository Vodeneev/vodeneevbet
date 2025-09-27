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

echo "🌐 Starting web interface..."
cd internal/api
go run main.go &
API_PID=$!

echo "📊 Starting parser (test mode)..."
cd ../parser
go run main.go -config ../../configs/local.yaml &
PARSER_PID=$!

echo ""
echo "✅ System started successfully!"
echo ""
echo "🌐 Web Interface: http://localhost:8081"
echo "📊 API Endpoints:"
echo "   - http://localhost:8081/api/odds"
echo "   - http://localhost:8081/api/matches"
echo ""
echo "📝 Logs:"
echo "   - Parser: logs/parser.log"
echo "   - API: logs/api.log"
echo ""
echo "🛑 To stop the system:"
echo "   - Press Ctrl+C"
echo "   - Or run: pkill -f 'go run main.go' && docker-compose down"
echo ""

# Wait for user interrupt
trap 'echo ""; echo "🛑 Stopping system..."; kill $API_PID $PARSER_PID 2>/dev/null; docker-compose down; echo "✅ System stopped"; exit 0' INT

# Keep script running
wait
