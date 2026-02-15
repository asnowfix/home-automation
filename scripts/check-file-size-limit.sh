#!/usr/bin/env bash
set -euo pipefail

MAX_BYTES="${MAX_BYTES:-1048576}"
IGNORE_REGEX="${IGNORE_REGEX:-^(dist/|bin/|\.git/)}"
CHECK_ALL_TRACKED="${CHECK_ALL_TRACKED:-0}"

failed=0

check_file() {
  local file="$1"

  if [[ "$file" =~ $IGNORE_REGEX ]]; then
    return
  fi

  if [ ! -f "$file" ]; then
    return
  fi

  local size
  size=$(wc -c <"$file" | tr -d ' ')
  if [ "$size" -gt "$MAX_BYTES" ]; then
    echo "ERROR: $file is $size bytes (limit: $MAX_BYTES bytes)" >&2
    failed=1
  fi
}

if [ "$CHECK_ALL_TRACKED" = "1" ]; then
  while IFS= read -r file; do
    check_file "$file"
  done < <(git ls-files)
else
  while IFS= read -r -d '' file; do
    check_file "$file"
  done < <(git diff --cached --name-only -z --diff-filter=ACMR)
fi

if [ "$failed" -ne 0 ]; then
  echo "Commit blocked: remove large files or use Git LFS." >&2
  exit 1
fi
