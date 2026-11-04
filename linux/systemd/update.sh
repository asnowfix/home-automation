#!/bin/bash
#
# Nightly self-update, run by myhome-update.timer at 00:00.
#
# Installs the .deb of GitHub's `releases/latest` (published, non-draft,
# non-prerelease -- also when GITHUB_TOKEN is set, so drafts are never picked).
#
# Release policy (maintainer, see #583): `latest` intentionally LAGS the newest
# release. A release is published and marked `latest` only after a manual
# install on the NAS plus one day in production. The fleet therefore exercises
# the DOWNGRADE path continuously: a host running a newer build than `latest`
# is expected to be moved back to it. Do NOT add a "never downgrade" rule. The
# v0.12.x -> v0.11.12 boundary was unsafe (pre-#458 KVS rename), which is why
# releases are drafted until validated. To keep a hand-installed build, disable
# myhome-update.timer first (see CLAUDE.md "NAS operational facts").
#
# Only an exact version match is skipped: reinstalling the same version re-runs
# postinst (daemon restart, timer re-enable, fleet-wide device-script push) for
# nothing (#611). Upgrades and downgrades proceed. Set FORCE=1 (or pass
# --force) to reinstall regardless.
#
# Test: linux/systemd/update_test.sh (run by `make test`).
set -ex
ORG=asnowfix
REPO=home-automation

FORCE="${FORCE:-}"
for arg in "$@"; do
    case "$arg" in
        --force) FORCE=1 ;;
    esac
done

ARCH=$(dpkg --print-architecture | sed 's/.*-//')
SUFFIX="${ARCH}.deb"
GITHUB_CURL_ARGS="-Ls -H Accept:application/vnd.github.v3.raw"
if [ -n "$GITHUB_TOKEN" ]; then
    GITHUB_CURL_ARGS="${GITHUB_CURL_ARGS} --oauth2-bearer $GITHUB_TOKEN"
fi

RELEASE=$(curl $GITHUB_CURL_ARGS "https://api.github.com/repos/${ORG}/${REPO}/releases/latest")
URL=$(echo "$RELEASE" | jq -r --arg suffix $SUFFIX '.assets[] | select(.name | endswith($suffix)) | .browser_download_url')
TARGET=$(echo "$RELEASE" | jq -r '.tag_name' | sed 's/^v//')
INSTALLED=$(dpkg-query -W -f='${Version}' myhome 2>/dev/null || true)

if [ -z "$URL" ]; then
    echo "No $SUFFIX asset in the latest release; nothing to do." >&2
    exit 1
fi

if [ -z "$FORCE" ] && [ "$INSTALLED" = "$TARGET" ]; then
    echo "Already at $TARGET, nothing to do." >&2
    exit 0
fi

echo "Fetching: $URL (installed: ${INSTALLED:-none}, target: $TARGET)" >&2

TMPDIR=$(mktemp -d)
pushd $TMPDIR
curl $GITHUB_CURL_ARGS -H 'Accept: application/octet-stream' -O "$URL"
echo "Installing: $(ls *$SUFFIX)" >&2
sudo dpkg -i *$SUFFIX
sudo apt-get install -f
popd
rm -rf "$TMPDIR"

echo "Update completed successfully." >&2
exit 0
