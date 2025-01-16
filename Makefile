OS ?= $(shell uname -s)
ME ?= $(shell id -un)

mods = $(wildcard */go.mod) $(wildcard */*/go.mod) $(wildcard */*/*/go.mod) $(wildcard */*/*/*/go.mod)

default: help

help:
	@echo make help build run install start stop

ifneq ($(MODULE),)
# make module MODULE=homectl/shelly/options
module:
	(mkdir -p $(MODULE) && cd $(MODULE) && go mod init $(MODULE)) && go work use $(MODULE)
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
	@echo $(mods)
	$(foreach m,$(mods),go work use $(dir $(m)) && (cd $(dir $(m)) && go mod tidy) &&) true

build run:
	$(MAKE) -C homectl $(@)
	$(MAKE) -C myhome $(@)
