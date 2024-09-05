OS ?= $(shell uname -s)

default: help

help:
	@echo make help build run install start stop

install:
	$(MAKE) -C myhome install .
ifeq ($(OS),Linux)
	cd linux && sudo install -m 644 -o root -g root myhome@.service /etc/systemd/system/myhome@.service
	sudo systemctl daemon-reload
	sudo systemctl enable myhome@$(shell id -un).service
	-systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

start:
ifeq ($(OS),Linux)
	sudo systemctl start myhome@$(shell id -un).service
	systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

stop:
ifeq ($(OS),Linux)
	sudo systemctl stop myhome@$(shell id -un).service
	-systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(@) for OS:$(OS))
endif

build run:
	$(MAKE) -C homectl $(@)
	$(MAKE) -C myhome $(@)
