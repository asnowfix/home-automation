package storage

import "myhome"

type Device struct {
	myhome.Device
	Info_   string `db:"info" json:"-"`
	Config_ string `db:"config" json:"-"`
}
