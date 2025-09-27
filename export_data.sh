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

# Run export utility
cd internal/export
go run main.go

# Move exports to project root
if [ -d "exports" ]; then
    echo "📁 Moving exports to project root..."
    cp exports/*.json ../../exports/ 2>/dev/null || true
    cp exports/*.csv ../../exports/ 2>/dev/null || true
    echo "✅ Export completed! Check the 'exports' directory."
else
    echo "❌ Export failed - no exports directory created"
    exit 1
fi

echo ""
echo "📁 Exported files:"
ls -la ../../exports/
