#pwsh
#PWD ?= $(shell (Get-Item .).FullName)
#cmd
PWD ?= $(shell cd)

GO_LDFLAGS += -X main.Program=$(notdir $(PWD))
GO_LDFLAGS += -X main.Repo=$(shell git config --get remote.$(shell git remote).url)
GO_LDFLAGS += -X main.Version=$(shell git describe --tags --exact-match 2>/dev/null || git describe --always)
GO_LDFLAGS += -X main.Commit=$(shell git rev-parse HEAD)

GOFLAGS = -ldflags="$(GO_LDFLAGS)"

build install run:
	go $(@) $(GOFLAGS) .