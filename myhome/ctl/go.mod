module myhome/ctl

go 1.23.0

require (
	github.com/spf13/cobra v1.8.1
	github.com/go-logr/logr v1.4.2
	myhome/ctl/follow v0.0.0-00010101000000-000000000000
	myhome/ctl/forget v0.0.0-00010101000000-000000000000
	myhome/ctl/group v0.0.0-00010101000000-000000000000
	myhome/ctl/list v0.0.0-00010101000000-000000000000
	myhome/ctl/mqtt v0.0.0-00010101000000-000000000000
	myhome/ctl/open v0.0.0-00010101000000-000000000000
	myhome/ctl/options v0.0.0-00010101000000-000000000000
	myhome/ctl/shelly v0.0.0-00010101000000-000000000000
	myhome/ctl/show v0.0.0-00010101000000-000000000000
	myhome/ctl/sswitch v0.0.0-00010101000000-000000000000
	global v0.0.0-00010101000000-000000000000
	hlog v0.0.0-00010101000000-000000000000
	myhome v0.0.0-00010101000000-000000000000
	mymqtt v0.0.0-00010101000000-000000000000
	pkg/shelly v0.0.0-00010101000000-000000000000
	pkg/shelly/types v0.0.0-00010101000000-000000000000
	debug v0.0.0-00010101000000-000000000000
)

replace myhome/ctl/follow => ./follow
replace myhome/ctl/forget => ./forget
replace myhome/ctl/group => ./group
replace myhome/ctl/list => ./list
replace myhome/ctl/mqtt => ./mqtt
replace myhome/ctl/open => ./open
replace myhome/ctl/options => ./options
replace myhome/ctl/shelly => ./shelly
replace myhome/ctl/show => ./show
replace myhome/ctl/sswitch => ./sswitch
replace global => ../../internal/global
replace hlog => ../../hlog
replace myhome => ../../internal/myhome
replace mymqtt => ../../mymqtt
replace pkg/shelly => ../../pkg/shelly
replace pkg/shelly/types => ../../pkg/shelly/types
replace debug => ../../internal/debug
