#!/bin/bash

# User Configuration
scan_dirs=("pkg")
ignore_paths=("ratelimit" "timing" "message" "logger")
ignore_wildcards=("")
root_files=("main.go")
allowed_extensions=("go")

# Function to build ignore clause
build_ignore_clause() {
    local clause=""
    for path in "${ignore_paths[@]}"; do
        clause="$clause -not -path \"*/${path}/*\""
    done
    for wildcard in "${ignore_wildcards[@]}"; do
        clause="$clause -not -path \"*/${wildcard}/*\""
    done
    echo "$clause"
}

# Function to build extension clause
build_extension_clause() {
    local clause=""
    for ext in "${allowed_extensions[@]}"; do
        if [ -n "$clause" ]; then
            clause="$clause -o"
        fi
        clause="$clause -name \"*.${ext}\""
    done
    echo "$clause"
}

# Find unique files from configured directories
find_files() {
    local all_files=()
    
    # Add root files
    for file in "${root_files[@]}"; do
        if [ -f "$file" ]; then
            all_files+=("$file")
        fi
    done
    
    # Add files from scan directories
    for dir in "${scan_dirs[@]}"; do
        if [ -d "$dir" ]; then
            local ignore_clause
            ignore_clause=$(build_ignore_clause)
            local ext_clause
            ext_clause=$(build_extension_clause)
            
            while IFS= read -r file; do
                if [ -n "$file" ]; then
                    all_files+=("$file")
                fi
            done < <(eval "find \"$dir\" $ignore_clause -type f \( $ext_clause \) 2>/dev/null")
        fi
    done
    
    # Output unique files, sorted
    printf '%s\n' "${all_files[@]}" | sort | uniq
}

# Function to normalize consecutive newlines
normalize_newlines() {
    sed -z 's/\n\{3,\}/\n\n/g'
}

# Function to strip comments from Go code using sed
strip_comments() {
    local file="$1"
    
    local temp_file
    temp_file=$(mktemp)

    cp "$file" "$temp_file"
    
    # Process the file to strip comments:
    # 1. Remove single-line comments (// ...)
    # 2. Remove multi-line comments (/* ... */)
    sed -i -e 's|//.*$||g' "$temp_file"
    
    # Handle multi-line comments
    awk '
        BEGIN { in_comment = 0 }
        {
            if (!in_comment) {
                # Check for start of comment
                if (match($0, "/\\*")) {
                    # If the comment starts and ends on the same line
                    if (match($0, "\\*/")) {
                        gsub("/\\*.*\\*/", "", $0)
                        print $0
                    } else {
                        # Comment starts but does not end on this line
                        print substr($0, 1, RSTART-1)
                        in_comment = 1
                    }
                } else {
                    # No comment on this line
                    print $0
                }
            } else {
                # We are in a comment, check for end
                if (match($0, "\\*/")) {
                    # Comment ends on this line
                    print substr($0, RSTART+2)
                    in_comment = 0
                }
                # Otherwise, line is entirely in comment, print nothing
            }
        }
    ' "$temp_file" > "${temp_file}.new"
    mv "${temp_file}.new" "$temp_file"
    
    # Remove empty lines and normalize whitespace
    sed -i '/^[[:space:]]*$/d' "$temp_file"

    # Output the processed file
    cat "$temp_file"

    rm "$temp_file"
}

temp_output_file=$(mktemp)

echo "=== File Structure ===" > "$temp_output_file"
find_files | sed 's/^\.\///' >> "$temp_output_file"

echo -e "\n=== File Contents ===" >> "$temp_output_file"

# Process each file
while IFS= read -r file; do
    echo -e "\n// FILE: ${file#./}" >> "$temp_output_file"
    
    if [[ "$file" == *.go ]]; then
        # For Go files, strip comments
        strip_comments "$file" >> "$temp_output_file"
    else
        # For other files, just cat the content
        cat "$file" >> "$temp_output_file"
    fi
    
    # Add separator
    echo "---" >> "$temp_output_file"
done < <(find_files)

# Remove the last separator
sed -i '$s/---$//' "$temp_output_file"

# Normalize newlines in the entire file
temp_normalized=$(mktemp)
normalize_newlines < "$temp_output_file" > "$temp_normalized"
mv "$temp_normalized" "$temp_output_file"

if command -v pbcopy &>/dev/null; then
    # macOS
    pbcopy < "$temp_output_file"
    echo "Output copied to clipboard (pbcopy)"
elif command -v xclip &>/dev/null; then
    # Linux
    xclip -selection clipboard < "$temp_output_file"
    echo "Output copied to clipboard (xclip)"
elif command -v clip.exe &>/dev/null; then
    # Windows
    clip.exe < "$temp_output_file"
    echo "Output copied to clipboard (clip.exe)"
else
    cat "$temp_output_file"
    echo "Clipboard copy not available, printing to console."
fi

# Clean up the temporary file
rm "$temp_output_file"