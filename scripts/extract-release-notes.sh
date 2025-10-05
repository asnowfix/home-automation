#!/bin/bash
set -eo pipefail

# Extract release notes for a specific version from RELEASE_NOTES.md
# Usage: ./extract-release-notes.sh [version]
# If no version is provided, uses the latest git tag

VERSION="${1:-}"

# If no version provided, get the latest tag
if [ -z "$VERSION" ]; then
    VERSION=$(git describe --tags --abbrev=0 2>/dev/null || echo "")
    if [ -z "$VERSION" ]; then
        echo "Error: No version specified and no git tags found" >&2
        exit 1
    fi
fi

# Remove 'v' prefix if present for matching
VERSION_NUM="${VERSION#v}"

# Look for version-specific file first
if [ -f "RELEASE_NOTES_v${VERSION_NUM}.md" ]; then
    cat "RELEASE_NOTES_v${VERSION_NUM}.md"
    exit 0
fi

# Otherwise, extract from RELEASE_NOTES.md
if [ ! -f "RELEASE_NOTES.md" ]; then
    echo "Error: No release notes file found for version ${VERSION}" >&2
    echo "Expected: RELEASE_NOTES_v${VERSION_NUM}.md or RELEASE_NOTES.md" >&2
    exit 1
fi

# Extract the section for this version from RELEASE_NOTES.md
# Look for "# Release Notes - vX.Y.Z" or "## vX.Y.Z" headers
# Stop at the next version header or end of file

awk -v version="${VERSION_NUM}" '
BEGIN { 
    found = 0
    in_section = 0
    # Skip template/preamble markers
    skip_template = 1
}

# Skip template preamble
/^# Release Notes Template/ { skip_template = 1; next }
/^> \*\*Note\*\*:/ { next }
/^## Release Information/ { skip_template = 1; next }
/^- \*\*Version\*\*: `vX\.Y\.Z`/ { next }
/^- \*\*Release Date\*\*: `YYYY-MM-DD`/ { next }
/^- \*\*Previous Version\*\*: `vX\.Y\.Z`/ { next }

# Look for version header (with or without "v" prefix)
/^# Release Notes - v?'"$version"'/ { 
    found = 1
    in_section = 1
    skip_template = 0
    print
    next
}

/^## v?'"$version"'/ {
    found = 1
    in_section = 1
    skip_template = 0
    print
    next
}

# Stop at next version header
in_section && /^# Release Notes - v?[0-9]+\.[0-9]+\.[0-9]+/ { exit }
in_section && /^## v?[0-9]+\.[0-9]+\.[0-9]+/ { exit }

# Print lines in the section (skip template markers)
in_section && !skip_template { print }

END {
    if (!found) {
        print "Error: No release notes found for version " version > "/dev/stderr"
        exit 1
    }
}
' RELEASE_NOTES.md
