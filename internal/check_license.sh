#!/usr/bin/env bash
set -ueo pipefail

CURRENT_YEAR=$(date +%Y)
export CURRENT_YEAR

# Function to update copyright year in Go files
update_go_copyright_year() {
    local file=$1
    local temp_file=$(mktemp)
    
    # Check if file has a copyright header
    if head -n3 "$file" | grep -q "Copyright.*20[0-9]\{2\}"; then
        # Update the year to current year in copyright line only
        echo "Processing file: $file"
        # Only replace year in Copyright lines, not throughout entire file
        sed "s/^Copyright 202[0-9]/Copyright $CURRENT_YEAR/" "$file" > "$temp_file"
    else
        # Add copyright header if missing
        echo "// Copyright $CURRENT_YEAR Adobe. All rights reserved." > "$temp_file"
        cat "$file" >> "$temp_file"
    fi
    
    # Replace original file with modified content
    mv "$temp_file" "$file"
}

# Function to update copyright year in LICENSE file
update_license_copyright_year() {
    local file=$1
    local temp_file=$(mktemp)
    
    echo "Processing LICENSE file"
    
    # Update only the line containing "Copyright 2022 Adobe"
    sed "s/Copyright 202[0-9] Adobe/Copyright $CURRENT_YEAR Adobe/g" "$file" > "$temp_file"
    
    # Replace original file with modified content
    mv "$temp_file" "$file"
}

export -f update_go_copyright_year
export -f update_license_copyright_year

# Update LICENSE file if it exists
if [ -f "LICENSE" ]; then
    update_license_copyright_year "LICENSE"
fi

# Find all Go files and update their copyright headers
find . -type f -iname '*.go' ! -path '*/vendor/*' -exec bash -c 'update_go_copyright_year "$1"' _ {} \;

# Check if any files are missing the license header
licRes=$(
    find . -type f -iname '*.go' ! -path '*/vendor/*' -exec \
         sh -c 'head -n3 $1 | grep -Eq "(Copyright|generated|GENERATED)" || echo "$1"' {} {} \;
)

if [ -n "${licRes}" ]; then
	echo -e "License header is missing in:\\n${licRes}"
	exit 255
fi
