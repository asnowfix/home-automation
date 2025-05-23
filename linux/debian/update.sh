#!/bin/bash
set -ex
REPO=asnowfix/home-automation
ARCH=$(dpkg --print-architecture)
TMPDIR=$(mktemp -d)
PACKAGE="myhome_${ARCH}.deb"
URL=$(curl -L -s https://api.github.com/repos/${REPO}/releases/latest | jq -r --arg suffix "${PACKAGE}" '.assets | .[] | select(.name | endswith($suffix)) | .browser_download_url')
curl -L -o $TMPDIR/$PACKAGE $URL
sudo dpkg -i "$TMPDIR/$PACKAGE"
rm -rf "$TMPDIR"
