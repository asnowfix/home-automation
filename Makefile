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

export GOTOOLCHAIN=go1.25.3

GOOS := $(shell go env GOOS)
GOARCH := $(shell go env GOARCH)

mods = $(patsubst %/,%,$(wildcard */go.mod) $(wildcard */*/go.mod) $(wildcard */*/*/go.mod) $(wildcard */*/*/*/go.mod))

# Local daemon dev loop: run/stop/tail a daemon instance built from whichever
# worktree invokes it, pointed at the real home MQTT broker. --instance local
# matches myhome-local.sh's convention (avoids RPC topic collisions with the
# systemd-managed "myhome" instance).
#
# State (pidfile/log) lives in a single OS-standard user directory, NOT under
# the repo/worktree: only one dev daemon should ever run at a time (it binds
# fixed ports 6080/6060), and a per-worktree location can't see a stale
# instance left running by a different worktree.
MYHOME_MQTT_BROKER ?= tcp://192.168.1.2:1883
ifeq ($(OS),Windows_NT)
LOCAL_DAEMON_STATE_DIR := $(LOCALAPPDATA)/myhome/local-daemon
else ifeq ($(OS),Darwin)
LOCAL_DAEMON_STATE_DIR := $(HOME)/Library/Application Support/myhome/local-daemon
else
XDG_STATE_HOME ?= $(HOME)/.local/state
LOCAL_DAEMON_STATE_DIR := $(XDG_STATE_HOME)/myhome/local-daemon
endif
LOCAL_DAEMON_PID := $(LOCAL_DAEMON_STATE_DIR)/myhome.pid
LOCAL_DAEMON_LOG := $(LOCAL_DAEMON_STATE_DIR)/myhome.log
LOCAL_DAEMON_PATTERN := daemon run --instance local --mqtt-broker $(MYHOME_MQTT_BROKER)

default: help

help:
	@echo "Available targets:"
	@echo "  help                  - Show this help message"
	@echo "  build                 - Build the project"
	@echo "  run                   - Run the project"
	@echo "  run-local-daemon      - Start a dev-loop daemon in the background (kills any prior instance first)"
	@echo "  stop-local-daemon     - Stop the dev-loop daemon started by run-local-daemon"
	@echo "  status-local-daemon   - Show whether the dev-loop daemon is running"
	@echo "  logs-local-daemon     - Tail the dev-loop daemon's log file"
	@echo "  install               - Install the project"
	@echo "  start                 - Start the service"
	@echo "  stop                  - Stop the service"
	@echo "  status                - Show service status"
	@echo "  logs                  - Show service logs"
	@echo "  test                  - Run tests across all workspace modules"
	@echo "  cover                 - Run tests with coverage; produces coverage.txt"
	@echo "  cover-report          - Print aggregate coverage total from coverage.txt"
	@echo "  cover-html            - Open coverage.txt as an HTML report in the browser"
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

release:
	goreleaser build --snapshot --clean --single-target
	./dist/snapshot_$(GOOS)_$(GOARCH)_v1/myhome version

run: build
	$(MAKE) -C myhome $(@)

# Dev-loop daemon: always kills any other instance before starting a fresh
# one built from the current checkout. Two layers of detection, since the
# pidfile alone can go stale (state dir wiped, process killed -9 elsewhere):
#   1. the shared pidfile — authoritative, works across worktrees/sessions
#   2. a pgrep fallback for a stray instance the pidfile doesn't know about
# The pgrep pattern also matches this very recipe's own shell (its command
# text contains the pattern string), so its own $$ (current shell PID) is
# excluded from the kill list.
run-local-daemon: build
	@mkdir -p "$(LOCAL_DAEMON_STATE_DIR)"
	@if [ -f "$(LOCAL_DAEMON_PID)" ]; then \
		OLD_PID=$$(cat "$(LOCAL_DAEMON_PID)"); \
		if kill -0 $$OLD_PID 2>/dev/null; then \
			echo "run-local-daemon: stopping existing instance (pid $$OLD_PID)"; \
			kill $$OLD_PID 2>/dev/null; sleep 1; kill -9 $$OLD_PID 2>/dev/null || true; \
		fi; \
	fi
	@PIDS="$$(pgrep -f '$(LOCAL_DAEMON_PATTERN)' 2>/dev/null | grep -v -x $$$$)"; \
	if [ -n "$$PIDS" ]; then \
		echo "run-local-daemon: stopping stray instance(s) not tracked by pidfile: $$PIDS"; \
		kill $$PIDS 2>/dev/null; sleep 1; kill -9 $$PIDS 2>/dev/null || true; \
	fi
	@rm -f "$(LOCAL_DAEMON_PID)"
	@(nohup ./myhome/myhome daemon run --instance local --mqtt-broker $(MYHOME_MQTT_BROKER) > "$(LOCAL_DAEMON_LOG)" 2>&1 & echo $$! > "$(LOCAL_DAEMON_PID)")
	@sleep 1
	@echo "run-local-daemon: started (pid $$(cat "$(LOCAL_DAEMON_PID)")), logs: $(LOCAL_DAEMON_LOG)"

stop-local-daemon:
	@if [ -f "$(LOCAL_DAEMON_PID)" ]; then \
		PID=$$(cat "$(LOCAL_DAEMON_PID)"); \
		if kill -0 $$PID 2>/dev/null; then kill $$PID && echo "stop-local-daemon: stopped (pid $$PID)"; else echo "stop-local-daemon: pidfile stale, not running"; fi; \
		rm -f "$(LOCAL_DAEMON_PID)"; \
	else \
		echo "stop-local-daemon: no pidfile ($(LOCAL_DAEMON_PID))"; \
	fi
	@PIDS="$$(pgrep -f '$(LOCAL_DAEMON_PATTERN)' 2>/dev/null | grep -v -x $$$$)"; \
	if [ -n "$$PIDS" ]; then kill $$PIDS 2>/dev/null; sleep 1; kill -9 $$PIDS 2>/dev/null || true; fi

status-local-daemon:
	@if [ -f "$(LOCAL_DAEMON_PID)" ] && kill -0 $$(cat "$(LOCAL_DAEMON_PID)") 2>/dev/null; then \
		echo "status-local-daemon: running (pid $$(cat "$(LOCAL_DAEMON_PID)"))"; \
	else \
		echo "status-local-daemon: not running"; \
	fi

logs-local-daemon:
	@tail -n 100 -f "$(LOCAL_DAEMON_LOG)"

test: build
	$(GO) test ./...
	@rc=0; for dir in $$(awk '/\t\.\//{sub(/\t\.\//, ""); print}' go.work); do \
	  if find $$dir \( -mindepth 1 -type d -exec test -f "{}/go.mod" \; -prune \) \
	          -o \( -type f -name "*_test.go" -print -quit \) 2>/dev/null | grep -q .; then \
	    (cd $$dir && $(GO) test ./...) || rc=1; \
	  fi; \
	done; exit $$rc

cover: build
	@mkdir -p coverage
	$(GO) test -covermode=atomic -coverprofile=coverage/root.cov ./...
	@rc=0; for dir in $$(awk '/\t\.\//{sub(/\t\.\//, ""); print}' go.work); do \
	  if find $$dir \( -mindepth 1 -type d -exec test -f "{}/go.mod" \; -prune \) \
	          -o \( -type f -name "*_test.go" -print -quit \) 2>/dev/null | grep -q .; then \
	    sdir=$$(echo $$dir | tr '/' '_'); \
	    (cd $$dir && $(GO) test -covermode=atomic -coverprofile=$(CURDIR)/coverage/$$sdir.cov ./...) || rc=1; \
	  fi; \
	done; \
	echo "mode: atomic" > coverage.txt; \
	for f in coverage/*.cov; do grep -v "^mode:" "$$f" >> coverage.txt 2>/dev/null || true; done; \
	exit $$rc

# stress: run timing-sensitive packages with GOMAXPROCS=2 and -count=10 to
# simulate CI-like goroutine starvation. Catches flaky tests that pass locally
# under full CPU but fail in CI where multiple packages compete for 2 cores.
# Run before pushing any test that uses time.Sleep or async protocol polling.
stress:
	GOMAXPROCS=2 $(GO) test -count=10 -timeout=300s ./internal/shelly/scripts/...

cover-report: coverage.txt
	$(GO) tool cover -func=coverage.txt | tail -1

cover-html: coverage.txt
	$(GO) tool cover -html=coverage.txt

# cover-min-suggest: prints the integer floor of the current aggregate
# coverage, i.e. the value to paste into .coverage-min after a PR that raises
# coverage. Run `make cover` first.
cover-min-suggest: coverage.txt
	@$(GO) tool cover -func=coverage.txt | awk '/^total:/{ split($$3, p, "."); print p[1] }'

build: generate
	$(MAKE) -C myhome $(@)

generate:
	$(GO) generate ./internal/myhome/ui/...
	$(GO) generate ./myhome/ctl/pool
	$(GO) generate ./myhome/ctl/garden
	$(GO) generate ./...

# Build Debian package for current OS/ARCH (Linux only)
# Usage: make debpkg [VERSION=X.Y.Z]
# If VERSION is not specified, uses git describe
ifeq ($(OS),Linux)
ARCH := $(shell dpkg --print-architecture 2>/dev/null || uname -m | sed 's/x86_64/amd64/' | sed 's/aarch64/arm64/')
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^v//')
DEBPKG_DIR := .debpkg
DEB_FILE := myhome_$(VERSION)_$(ARCH).deb

debpkg: build debpkg-package

# debpkg-package assembles the .deb from an already-built myhome/myhome
# binary, without (re)triggering `build`/`generate`. Split out so that
# cross-compiling CI jobs can run `make generate` and a GOARCH-scoped
# `make -C myhome build` themselves — see .goreleaser.yml's `before.hooks`
# for why: `go generate` invokes host-native helper tools (e.g. fetchasset)
# that must NOT inherit a cross GOARCH, while the final binary must.
debpkg-package:
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
	@# Helper scripts and configuration
	mkdir -p $(DEBPKG_DIR)/usr/share/myhome
	cp linux/systemd/update.sh $(DEBPKG_DIR)/usr/share/myhome/update.sh
	cp linux/systemd/myhome-db-backup.sh $(DEBPKG_DIR)/usr/share/myhome/myhome-db-backup.sh
	cp myhome-example.yaml $(DEBPKG_DIR)/usr/share/myhome/myhome-example.yaml
	cp linux/prometheus/mqtt-exporter.yaml.sample $(DEBPKG_DIR)/usr/share/myhome/prometheus-mqtt-exporter.yaml.sample
	chmod +x $(DEBPKG_DIR)/usr/share/myhome/*.sh
	@# Prometheus MQTT Exporter: systemd drop-in pointing it at our own config
	@# path, instead of /etc/prometheus/mqtt-exporter.yaml (owned by the
	@# upstream prometheus-mqtt-exporter package — see #261)
	mkdir -p $(DEBPKG_DIR)/lib/systemd/system/prometheus-mqtt-exporter.service.d
	cp linux/prometheus/myhome.conf $(DEBPKG_DIR)/lib/systemd/system/prometheus-mqtt-exporter.service.d/myhome.conf
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
	@echo "Recommends: prometheus-mqtt-exporter" >> $(DEBPKG_DIR)/DEBIAN/control
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