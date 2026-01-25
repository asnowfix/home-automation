OS ?= $(shell uname -s)
ifneq ($(WSL_DISTRO_NAME),)
ME ?= $(shell /mnt/c/windows/system32/cmd.exe /c whoami 2>/dev/null)
else
ME ?= $(shell whoami)
endif
$(info OS=$(OS) ME=$(ME))

ifeq ($(OS),Windows_NT)
# SHELL := powershell.exe
# SHELL := cmd.exe
# GO := "c:\\Program Files\\Go\\bin\\go.exe"
# folder = $(subst /,\\,$1)
SHELL := bash.exe
GO := "/mnt/c/Program Files/Go/bin/go.exe"
folder = $1
else
GO := go
folder = $1
endif

mods = $(patsubst %/,%,$(wildcard */go.mod) $(wildcard */*/go.mod) $(wildcard */*/*/go.mod) $(wildcard */*/*/*/go.mod))

default: help

help:
	@echo "Available targets:"
	@echo "  help                  - Show this help message"
	@echo "  build                 - Build the project"
	@echo "  run                   - Run the project"
	@echo "  install               - Install the project"
	@echo "  start                 - Start the service"
	@echo "  stop                  - Stop the service"
	@echo "  status                - Show service status"
	@echo "  logs                  - Show service logs"
	@echo "  tidy                  - Tidy Go modules"
	@echo "  debpkg                - Build Debian package (Linux only, VERSION=X.Y.Z optional)"
	@echo "  upload-release-notes  - Upload release notes to GitHub (VERSION=vX.Y.Z optional)"

ifneq ($(MODULE),)
# make module MODULE=myhome/ctl/options
module:
	(mkdir -p $(MODULE) && cd $(MODULE) && $(GO) mod init $(MODULE)) && $(GO) work use $(MODULE)
endif

install:
	$(MAKE) -C myhome install .
ifeq ($(OS),Linux)
	cd linux && sudo install -m 644 -o root -g root myhome@.service /etc/systemd/system/myhome@.service
	sudo systemctl daemon-reload
	sudo systemctl enable myhome@$(ME).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

status:
ifeq ($(OS),Linux)
	systemctl status myhome@$(ME).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

start:
ifeq ($(OS),Linux)
	mkdir -p $(HOME)/.local/state/myhome
	sudo systemctl start myhome@$(ME).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

stop:
ifeq ($(OS),Linux)
	sudo systemctl stop myhome@$(ME).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

status:
ifeq ($(OS),Linux)
	systemctl status myhome@$(ME).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

logs:
ifeq ($(OS),Linux)
	journalctl -u myhome@$(ME).service -f
else
	$(error unsupported $(@) for OS:$(OS))
endif

tidy:
	$(GO) get -u ./...
	$(GO) mod tidy
	$(foreach m,$(mods),$(GO) work use $(dir $(m)) &&) echo
	$(foreach m,$(mods),(cd $(call folder,$(dir $(m))) && $(GO) list -m -u all) &&) echo
	$(foreach m,$(mods),(cd $(call folder,$(dir $(m))) && $(GO) get -u ./...) &&) echo
	$(foreach m,$(mods),(cd $(call folder,$(dir $(m))) && $(GO) mod tidy) &&) echo

build run:
	$(MAKE) -C myhome $(@)

# Build Debian package for current OS/ARCH (Linux only)
# Usage: make debpkg [VERSION=X.Y.Z]
# If VERSION is not specified, uses git describe
ifeq ($(OS),Linux)
ARCH := $(shell dpkg --print-architecture 2>/dev/null || uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
DEBPKG_DIR := .debpkg
DEB_FILE := myhome_$(VERSION)_$(ARCH).deb

debpkg: build
	@echo "Building Debian package for $(ARCH), version $(VERSION)..."
	@# Clean previous build
	rm -rf $(DEBPKG_DIR)
	@# Main program
	mkdir -p $(DEBPKG_DIR)/usr/bin
	cp myhome/myhome $(DEBPKG_DIR)/usr/bin/myhome
	@# Systemd units
	mkdir -p $(DEBPKG_DIR)/lib/systemd/system
	cp linux/systemd/myhome.service $(DEBPKG_DIR)/lib/systemd/system/
	cp linux/systemd/myhome-update.service $(DEBPKG_DIR)/lib/systemd/system/
	cp linux/systemd/myhome-update.timer $(DEBPKG_DIR)/lib/systemd/system/
	cp linux/systemd/myhome-db-backup.service $(DEBPKG_DIR)/lib/systemd/system/
	cp linux/systemd/myhome-db-backup.timer $(DEBPKG_DIR)/lib/systemd/system/
	@# Helper scripts
	mkdir -p $(DEBPKG_DIR)/usr/share/myhome
	cp linux/systemd/update.sh $(DEBPKG_DIR)/usr/share/myhome/update.sh
	cp linux/systemd/myhome-db-backup.sh $(DEBPKG_DIR)/usr/share/myhome/myhome-db-backup.sh
	chmod +x $(DEBPKG_DIR)/usr/share/myhome/*.sh
	@# DEBIAN maintainer scripts
	mkdir -p $(DEBPKG_DIR)/DEBIAN
	cp linux/debian/postinst.sh $(DEBPKG_DIR)/DEBIAN/postinst
	cp linux/debian/prerm.sh $(DEBPKG_DIR)/DEBIAN/prerm
	cp linux/debian/postrm.sh $(DEBPKG_DIR)/DEBIAN/postrm
	chmod +x $(DEBPKG_DIR)/DEBIAN/postinst $(DEBPKG_DIR)/DEBIAN/prerm $(DEBPKG_DIR)/DEBIAN/postrm
	@# Create control file
	@echo "Package: myhome" > $(DEBPKG_DIR)/DEBIAN/control
	@echo "Version: $(VERSION)" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Section: utils" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Priority: optional" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Architecture: $(ARCH)" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Depends: libc6 (>= 2.2.1), systemd, jq, curl" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Maintainer: Francois-Xavier 'FiX' KOWALSKI <fix.kowalski@gmail.com>" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Description: MyHome Automation" >> $(DEBPKG_DIR)/DEBIAN/control
	@echo " Home automation daemon and CLI tools." >> $(DEBPKG_DIR)/DEBIAN/control
	@echo "Homepage: https://github.com/asnowfix/home-automation" >> $(DEBPKG_DIR)/DEBIAN/control
	@# Build the package
	dpkg-deb --build --root-owner-group $(DEBPKG_DIR) $(DEB_FILE)
	@echo "✓ Built $(DEB_FILE)"
	@# Cleanup
	rm -rf $(DEBPKG_DIR)
else
debpkg:
	$(error debpkg target is only available on Linux)
endif

push:
	$(GIT) push

msi: build-msi
	$msi = "$out\MyHome-0.0.18.msi"
	go-msi make --msi $msi --version 0.0.18 --path .\wix.json --arch amd64 --license .\LICENSE --out $out

# Upload release notes for the latest version to GitHub
# Usage: make upload-release-notes [VERSION=vX.Y.Z]
# If VERSION is not specified, uses the latest git tag
# Extracts the specific version section from RELEASE_NOTES.md (no template/preamble)
upload-release-notes:
	@echo "Uploading release notes to GitHub..."
	@echo "Fetching latest tags..."; \
	git fetch --tags --quiet; \
	VERSION=$${VERSION:-$$(git describe --tags --abbrev=0 2>/dev/null)}; \
	if [ -z "$$VERSION" ]; then \
		echo "Error: No version specified and no git tags found" >&2; \
		exit 1; \
	fi; \
	echo "Version: $$VERSION"; \
	if ! command -v gh &> /dev/null; then \
		echo "Error: GitHub CLI (gh) is not installed" >&2; \
		echo "Install it from: https://cli.github.com/" >&2; \
		exit 1; \
	fi; \
	echo "Extracting release notes for $$VERSION..."; \
	TEMP_NOTES=$$(mktemp); \
	if ./scripts/extract-release-notes.sh "$$VERSION" > "$$TEMP_NOTES" 2>&1; then \
		echo "Updating release $$VERSION on GitHub..."; \
		gh release edit "$$VERSION" --notes-file "$$TEMP_NOTES" && \
		echo "✓ Successfully uploaded release notes for $$VERSION"; \
		rm -f "$$TEMP_NOTES"; \
	else \
		cat "$$TEMP_NOTES" >&2; \
		rm -f "$$TEMP_NOTES"; \
		exit 1; \
	fi

.PHONY: upload-release-notes