module myhome/ui

go 1.24.2

require (
	myhome v0.0.0-00010101000000-000000000000
	myhome/storage v0.0.0-00010101000000-000000000000
)

replace myhome/storage => ../../../myhome/storage

replace myhome => ../
