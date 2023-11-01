default: help

help:
	@echo make help install start stop

install:
	cd myhome && go install .
ifeq ($(shell uname -s),Linux)
	cd linux && sudo install -m 644 -o root -g root myhome@.service /etc/systemd/system/myhome@.service
	sudo systemctl daemon-reload
	sudo systemctl enable myhome@$(shell id -un).service
	-systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(shell uname -s))
endif

start:
ifeq ($(shell uname -s),Linux)
	sudo systemctl start myhome@$(shell id -un).service
	systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(shell uname -s))
endif

stop:
ifeq ($(shell uname -s),Linux)
	sudo systemctl stop myhome@$(shell id -un).service
	-systemctl status myhome@$(shell id -un).service
else
	$(error unsupported $(shell uname -s))
endif
