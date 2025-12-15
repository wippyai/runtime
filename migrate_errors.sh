#!/bin/bash

# Script to migrate error definitions from api/service/* to service/*

# Function to migrate a single error file
migrate_error_file() {
    local api_path="$1"
    local service_path="$2"
    local old_import="$3"
    local new_import="$4"

    echo "Migrating: $api_path -> $service_path"

    # Check if api error file exists
    if [ ! -f "$api_path" ]; then
        echo "  Skipping: API error file not found"
        return
    fi

    # Check if service directory exists
    local service_dir=$(dirname "$service_path")
    if [ ! -d "$service_dir" ]; then
        echo "  Creating directory: $service_dir"
        mkdir -p "$service_dir"
    fi

    # Copy error file to service directory
    if [ -f "$service_path" ]; then
        echo "  Warning: Service error file already exists, will merge"
        # For now, skip if exists - we'll handle merges manually
        return
    else
        echo "  Copying error file"
        cp "$api_path" "$service_path"
    fi

    # Find all files that import the old path
    echo "  Finding files that use old import..."
    local files=$(grep -r -l "$old_import" --include="*.go" . 2>/dev/null | grep -v "/api/service/")

    if [ -z "$files" ]; then
        echo "  No files found using this import"
        return
    fi

    # Update imports in each file
    for file in $files; do
        echo "    Updating: $file"
        # Update the import statement
        sed -i "s|$old_import|$new_import|g" "$file"
    done

    echo "  Migration complete"
}

cd wippy

# Migrate api/service/aws/s3/errors.go
migrate_error_file \
    "api/service/aws/s3/errors.go" \
    "service/aws/s3/errors.go" \
    "github.com/wippyai/runtime/api/service/aws/s3" \
    "github.com/wippyai/runtime/service/aws/s3"

# Migrate api/service/di/errors.go
migrate_error_file \
    "api/service/di/errors.go" \
    "service/di/errors.go" \
    "github.com/wippyai/runtime/api/service/di" \
    "github.com/wippyai/runtime/service/di"

# Migrate api/service/exec/errors.go
migrate_error_file \
    "api/service/exec/errors.go" \
    "service/exec/errors.go" \
    "github.com/wippyai/runtime/api/service/exec" \
    "github.com/wippyai/runtime/service/exec"

# Migrate api/service/fs/directory/errors.go
migrate_error_file \
    "api/service/fs/directory/errors.go" \
    "service/fs/directory/errors.go" \
    "github.com/wippyai/runtime/api/service/fs/directory" \
    "github.com/wippyai/runtime/service/fs/directory"

# Migrate api/service/host/errors.go
migrate_error_file \
    "api/service/host/errors.go" \
    "service/host/errors.go" \
    "github.com/wippyai/runtime/api/service/host" \
    "github.com/wippyai/runtime/service/host"

echo "All migrations complete"
