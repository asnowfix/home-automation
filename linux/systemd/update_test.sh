#!/bin/bash
# Decision tests for update.sh with stubbed dpkg-query/curl/dpkg/sudo/apt-get.
# Needs bash and jq. Run by `make test`.
set -u
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
mkdir "$W/bin"

cat > "$W/bin/dpkg" <<'S'
#!/bin/bash
if [ "$1" = "--print-architecture" ]; then echo arm64; exit 0; fi
echo "dpkg $*" >> "$STUB_LOG"
S
cat > "$W/bin/dpkg-query" <<'S'
#!/bin/bash
[ -n "${STUB_INSTALLED:-}" ] || exit 1
printf '%s' "$STUB_INSTALLED"
S
cat > "$W/bin/sudo" <<'S'
#!/bin/bash
exec "$@"
S
cat > "$W/bin/apt-get" <<'S'
#!/bin/bash
echo "apt-get $*" >> "$STUB_LOG"
S
cat > "$W/bin/curl" <<'S'
#!/bin/bash
echo "curl $*" >> "$STUB_LOG"
last="${@: -1}"
case "$*" in
  *" -O "*) touch "$(basename "$last")" ;;
  *releases/latest) echo "{\"tag_name\":\"v$STUB_TARGET\",\"assets\":[{\"name\":\"myhome_${STUB_TARGET}_arm64.deb\",\"browser_download_url\":\"https://example.invalid/myhome_${STUB_TARGET}_arm64.deb\"}]}" ;;
  *) echo "unexpected curl: $*" >&2; exit 1 ;;
esac
S
chmod +x "$W/bin/"*

fail=0
# run <name> <installed> <target> <expect-dpkg-i: yes|no> <command...>
run() {
    local name=$1 installed=$2 target=$3 expect=$4; shift 4
    : > "$W/log"
    env PATH="$W/bin:$PATH" STUB_LOG="$W/log" STUB_INSTALLED="$installed" STUB_TARGET="$target" "$@" \
        >"$W/out" 2>&1
    local rc=$? got=no
    grep -q '^dpkg -i' "$W/log" && got=yes
    if [ $rc -ne 0 ] || [ "$got" != "$expect" ]; then
        echo "FAIL $name: rc=$rc dpkg-i=$got want=$expect"; sed 's/^/  | /' "$W/out"; fail=1
    else
        echo "ok   $name"
    fi
}

run skip-same     0.12.4 0.12.4  no  bash "$HERE/update.sh"
run upgrade       0.12.4 0.12.5  yes bash "$HERE/update.sh"
run downgrade     0.12.5 0.11.12 yes bash "$HERE/update.sh"
run not-installed ""     0.12.4  yes bash "$HERE/update.sh"
run force-env     0.12.4 0.12.4  yes FORCE=1 bash "$HERE/update.sh"
run force-arg     0.12.4 0.12.4  yes bash "$HERE/update.sh" --force

# Token branch must use releases/latest (never the newest-created, possibly draft, release).
run token-skip    0.12.4 0.12.4  no  GITHUB_TOKEN=x bash "$HERE/update.sh"
if grep -q 'releases?per_page' "$W/log"; then echo "FAIL token branch listed releases"; fail=1; fi
if ! grep -q 'releases/latest' "$W/log"; then echo "FAIL token branch did not use releases/latest"; fail=1; fi

exit $fail
