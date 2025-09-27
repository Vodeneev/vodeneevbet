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
rm -f exports/*.json exports/*.csv 2>/dev/null || true
echo "✅ Previous exports cleaned"

# Run export utility
cd internal/export
go run main.go

# Move exports to project root
if [ -d "exports" ]; then
    echo "📁 Moving exports to project root..."
    # Clean target directory first
    EXPORTS_DIR="../../exports"
    # Get absolute path
    ABS_EXPORTS_DIR=$(cd "$EXPORTS_DIR" && pwd)
    for file in "$ABS_EXPORTS_DIR"/*.json; do
        [ -f "$file" ] && rm -f "$file"
    done
    for file in "$ABS_EXPORTS_DIR"/*.csv; do
        [ -f "$file" ] && rm -f "$file"
    done
    # Copy new files
    cp exports/*.json "$EXPORTS_DIR/" 2>/dev/null || true
    cp exports/*.csv "$EXPORTS_DIR/" 2>/dev/null || true
    echo "✅ Export completed! Check the 'exports' directory."
else
    echo "❌ Export failed - no exports directory created"
    exit 1
fi

echo ""
echo "📁 Exported files:"
ls -la ../../exports/
