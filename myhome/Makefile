#pwsh
#PWD ?= $(shell (Get-Item .).FullName)
#cmd
PWD ?= $(shell cd)

VERSION_PKG := github.com/asnowfix/home-automation/pkg/version

GO_LDFLAGS += -X $(VERSION_PKG).Program=$(notdir $(PWD))
GO_LDFLAGS += -X $(VERSION_PKG).Repo=$(shell git config --get remote.$(shell git remote).url)
GO_LDFLAGS += -X $(VERSION_PKG).Version=$(shell git describe --tags --exact-match 2>/dev/null || git describe --always)$(shell git diff-index --quiet HEAD -- || echo '-dirty')
GO_LDFLAGS += -X $(VERSION_PKG).Commit=$(shell git rev-parse HEAD)

GOFLAGS = -ldflags="$(GO_LDFLAGS)"

build install run:
	go $(@) $(GOFLAGS) .