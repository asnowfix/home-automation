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
	@echo make help build run install start stop

ifneq ($(MODULE),)
# make module MODULE=homectl/options
module:
	(mkdir -p $(MODULE) && cd $(MODULE) && $(GO) mod init $(MODULE)) && $(GO) work use $(MODULE)
endif

install:
	$(MAKE) -C myhome install .
ifeq ($(OS),Linux)
	cd linux && sudo install -m 644 -o root -g root myhome@.service /etc/systemd/system/myhome@.service
	sudo systemctl daemon-reload
	sudo systemctl enable myhome@$(ME).service
	-systemctl status myhome@$(ME).service
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
	$(foreach m,$(mods),$(GO) work use $(dir $(m)) &&) echo
	$(foreach m,$(mods),(cd $(call folder,$(dir $(m))) && $(GO) mod tidy) &&) echo

build run:
	$(MAKE) -C homectl $(@)
	$(MAKE) -C myhome $(@)

push:
	$(GIT) push

msi: build-msi
	$msi = "$out\MyHome-0.0.18.msi"
	go-msi make --msi $msi --version 0.0.18 --path .\wix.json --arch amd64 --license .\LICENSE --out $out