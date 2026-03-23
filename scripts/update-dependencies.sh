#!/bin/bash

echo "Updating Go dependencies for Go 1.24 compatibility..."

# Update controller-runtime and related dependencies
go get sigs.k8s.io/controller-runtime@v0.17.0
go get k8s.io/api@v0.29.0
go get k8s.io/apimachinery@v0.29.0
go get k8s.io/client-go@v0.29.0

# Clean up and update all dependencies
go mod tidy

echo "✅ Dependencies updated!"
echo ""
echo "Now try running:"
echo "  controller-gen object paths=\"./api/...\""
echo "  go run cmd/main.go"