#!/bin/bash
set -ex
ORG=asnowfix
REPO=home-automation

ARCH=$(dpkg --print-architecture | sed 's/.*-//')
SUFFIX="${ARCH}.deb"
GITHUB_CURL_ARGS="-Ls -H Accept:application/vnd.github.v3.raw"
if [ -n "$GITHUB_TOKEN" ]; then
    GITHUB_CURL_ARGS="${GITHUB_CURL_ARGS} --oauth2-bearer $GITHUB_TOKEN"
fi

URL=$(curl $GITHUB_CURL_ARGS "https://api.github.com/repos/${ORG}/${REPO}/releases/latest" | 
    jq -r --arg suffix $SUFFIX '.assets[] | select(.name | endswith($suffix)) | .browser_download_url')
echo "Fetching: $URL" >&2

TMPDIR=$(mktemp -d)
pushd $TMPDIR
curl $GITHUB_CURL_ARGS -H 'Accept: application/octet-stream' -O "$URL"
echo "Installing: $(ls *$SUFFIX)" >&2
sudo dpkg -i *$SUFFIX
apt-get install -f
popd
rm -rf "$TMPDIR"

echo "Restarting service..." >&2
systemctl daemon-reload
systemctl enable myhome
systemctl restart myhome
systemctl status myhome

echo "Update completed successfully." >&2
exit 0

