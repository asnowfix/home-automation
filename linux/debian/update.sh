#!/bin/bash
set -ex
REPO=asnowfix/home-automation
ARCH=$(dpkg --print-architecture | sed 's/.*-//')

TMPDIR=$(mktemp -d)
pushd $TMPDIR
SUFFIX="${ARCH}.deb"
URL=$(curl -L -s https://api.github.com/repos/${REPO}/releases/latest | jq -r --arg suffix "${SUFFIX}" '.assets | .[] | select(.name | endswith($suffix)) | .browser_download_url')
curl -L -o pkg.deb $URL
sudo dpkg -i pkg.deb
popd
rm -rf "$TMPDIR"

sudo apt-get install -f
sudo systemctl daemon-reload
sudo systemctl enable myhome
sudo systemctl restart myhome
sudo systemctl status myhome
