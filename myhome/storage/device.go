package storage

import "github.com/asnowfix/home-automation/internal/myhome"

type Device struct {
	myhome.Device
	Info_   string `db:"info" json:"-"`
	Config_ string `db:"config" json:"-"`
}
