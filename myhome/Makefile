#pwsh
#PWD ?= $(shell (Get-Item .).FullName)
#cmd
PWD ?= $(shell cd)

VERSION_PKG := github.com/asnowfix/home-automation/pkg/version

GO_LDFLAGS += -X $(VERSION_PKG).Program=$(notdir $(PWD))
GO_LDFLAGS += -X $(VERSION_PKG).Repo=$(shell git config --get remote.$(shell git remote).url)
# --match 'v*' restricts `git describe` to release-version tags: the
# integration-branch progress tags (`campaign/<issue>-<slug>`, see #368) are
# not version numbers and must never be picked up as the embedded version
# or as a packaging version (they broke both before this fix).
GO_LDFLAGS += -X $(VERSION_PKG).Version=$(shell git describe --tags --match 'v*' --exact-match 2>/dev/null || git describe --tags --match 'v*' --always)$(shell git diff-index --quiet HEAD -- || echo '-dirty')
GO_LDFLAGS += -X $(VERSION_PKG).Commit=$(shell git rev-parse HEAD)

GOFLAGS = -ldflags="$(GO_LDFLAGS)"

build install run:
	go $(@) $(GOFLAGS) .