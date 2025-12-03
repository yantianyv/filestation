#!/bin/bash

# Build Go application
echo "Building Go application..."

# Output filename
OUTPUT_NAME="filestation"

# Build
go build -o ${OUTPUT_NAME} main.go

if [ $? -eq 0 ]; then
    echo "Build complete! Output: ${OUTPUT_NAME}"
else
    echo "Build failed!"
    exit 1
fi
