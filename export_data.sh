#!/bin/bash

echo "📊 Exporting data from YDB..."
echo "============================="

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    echo "❌ Please run this script from the project root directory"
    exit 1
fi

# Create exports directory in project root
mkdir -p exports

# Clean previous exports
echo "🧹 Cleaning previous exports..."
rm -rf exports 2>/dev/null || true
echo "✅ Previous exports cleaned"

# Create exports directory
mkdir -p exports

# Run export utility
go run ./cmd/tools/export -config configs/local.yaml

echo ""
echo "📁 Exported files:"
ls -la exports/ 2>/dev/null || echo "No exports directory found"
