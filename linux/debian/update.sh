#!/bin/bash
set -ex
REPO=asnowfix/home-automation
ARCH=$(dpkg --print-architecture)
TMPDIR=$(mktemp -d)
PACKAGE="myhome_.deb"
URL=$(curl -L -s https://api.github.com/repos/${REPO}/releases/latest | jq -r --arg suffix "${ARCH}.deb" '.assets | .[] | select(.name | endswith($suffix)) | .browser_download_url')
curl -L -o $TMPDIR/$PACKAGE $URL
sudo dpkg -i "$TMPDIR/$PACKAGE"
rm -rf "$TMPDIR"
