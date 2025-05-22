TODO
====

License
-------

[ ] Change license from MPL-2.0 to MIT/BSD when ready
[ ] Add SPDX license identifier everywhere

Model
-----

[ ] replace dependency of myhome.* on shelly.* by interfaces

Functions
---------

[ ] BUG fix on Windows: 'Failed to install event source: Access is denied.'
[ ] BUG fix wrong HTTP verb:

        1:23PM INF ..\pkg\shelly\shttp\channel.go:81 > Calling method=GET url=http://192.168.1.40/rpc/Shelly.GetDeviceInfo v=0
        1:23PM INF ..\pkg\shelly\shttp\channel.go:94 > status code code=200 v=0
        1:23PM INF ..\pkg\shelly\ops.go:177 > Calling channel=http out_type=*shelly.ComponentsResponse params={"keys":["config","status"]} v=0
        1:23PM ERR ..\pkg\shelly\shttp\channel.go:69 > Params error error error="GET support query parameters only (got *shelly.ComponentsRequest)"
        1:23PM ERR ..\pkg\shelly\shttp\channel.go:38 > HTTP error error="GET support query parameters only (got *shelly.ComponentsRequest)"
        1:23PM ERR ..\internal\myhome\device.go:231 > Unable to get device's components (continuing) error="GET support query parameters only (got *shelly.ComponentsRequest)" logger=DeviceManager#WatchZeroConf

[ ] Deactivate Wi-Fi if Ethernet is available & active.
[x] Disable auto-off timer of the pool-house switches (using double push)
[ ] Upload ip-assignment-watchdog.js on every device that have scripting
[ ] Configure adaptive heater control on every device that are known to be heaters (based on group membership)
[ ] Upload daily-reboot.js on every device that have scripting
[ ] Turn on/off heaters based on kalman filter and <https://developer.accuweather.com>
[ ] Daily reboot script to upload everywhere (inspired by <https://github.com/ALLTERCO/shelly-script-examples>)
[x] BUG make homectl `forget` actually work (right now it does not seem to update the DB storage)
[ ] BUG ZeroConf scanning (automatic resolver) not working on Windows or macOS
[ ] BUG ZeroConf scanning stops working after a while (few minutes)
[ ] Support matter protocol for Gen3/4 devices
[x] BUG Group ID's (integers) do not increment
[x] BUG no timeout if there is no myhome instance running
[x] ability to change device name
[x] Check/force MQTT configuration
[x] Get IP addresses in the 'host' column of the 'devices' table
[ ] Create/Configure scripts in a single operation
[x] Status for one / multiple scripts in a single operation
[ ] Run simple JavaScript in a single operation
[ ] Do not scan ZeroConf when devices are explicit
[ ] BUG: Find mqtt.local. using mDNS in homectl
[x] HTTP POST (spurious "config" layer)
[x] Fix MQTT when several CallE() invocations are in the same run
[ ] Add support for linksys velop devices (via JNAP protocol) 

    - https://github.com/uvjim/linksys_velop
    - https://github.com/uvjim/linksys_velop/blob/master/README.md

[x] Re-enable mDNS for early devices discovery
[x] Use ZeroConf to discover (quickly) MQTT broker
[x] Configure MQTT broker immediatelly after device discovery
[x] Resolve hostname using mDNS, on systems (eg. Windows) that do not have it in their system resolvers
[x] Publish mqtt.local. using mDNS
[ ] Publish myhome.local. ("penates.local."?) using mDNS
[ ] Rework file/folder layout to be more generic using <https://github.com/golang-standards/project-layout>
[ ] Move homectl as ctl subcommand of myhome
[ ] Find out proper layout

    - <https://medium.com/@potto_94870/understand-mdns-with-an-example-1e05ef70013b>
    - <https://andrewdupont.net/2022/01/27/using-mdns-aliases-within-your-home-network/>
    - Consider using Pion mDNS <https://github.com/pion/mdns> or HashiCorp mDNS <https://github.com/hashicorp/mdns/blob/main/server.go> rather than ZeroConf.
    - <https://dave.cheney.net/2011/10/15/scratching-my-own-itch-or-how-to-publish-multicast-dns-records-in-go> and with supporting repo fork follow-up <https://github.com/ugjka/mdns>

[ ] Add interactive shell
[ ] Add Amazon Alexa integration with <https://github.com/ericdaugherty/alexa-skills-kit-golang?tab=readme-ov-file>
[ ] Add Google Home integration
[ ] Add Home Assistant integration

        - https://www.dolthub.com/blog/2023-03-29-interactive-shell-golang/
        - https://github.com/abiosoft/ishell

[x] Use pure-Go sqlite implementation (to allow cross-compilation)

        - https://pkg.go.dev/modernc.org/sqlite
        - https://github.com/ncruces/go-sqlite3
        - https://github.com/cvilsmeier/go-sqlite-bench

[x] Re-init list of live devices at startup... or lazy version?
[x] Timeout on missing/non-responsive devices/server
[x] Ctrl-C should stop myhome program (whatever the option)
[x] Fix inbound IPv6 communication

Cleanup
-------

[ ] Code review by Windsurf
[ ] Auto-Stop script at upload if running

        shelly_notification:164 Status change of script:1: {"error_msg":null,"errors":[],"running":true}

[ ] Use Native slog.in-context like:

        ```go
	ctx = logr.NewContext(ctx, logr.New(logr.NewJSONEncoder()))

        [...]

	log, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}
        ctx = slog.NewContext(ctx, slog.New(slog.NewTextHandler(os.Stdout, nil)))
        ```

[ ] Reduce MQTT traffic (prefer using device lookup result when possible)

        Wifi.GetStatus via MQTT" msg-count=23054 timestamp=87339.946 v=0
        Shelly.GetComponents via SHC 35.205.206.219:6022" msg-count=23055 timestamp=87339.946 v=0
        Shelly.GetComponents via SHC 35.205.206.219:6022" msg-count=23056 timestamp=87340.148 v=0
        Wifi.GetStatus via MQTT" msg-count=23057 timestamp=87340.834 v=0
        Shelly.GetDeviceInfo via MQTT" msg-count=23058 timestamp=87346.675 v=0
        Shelly.ListMethods via MQTT" msg-count=23059 timestamp=87347.041 v=0
        Script.List via MQTT" msg-count=23060 timestamp=87347.137 v=0
        Shelly.GetDeviceInfo via MQTT" msg-count=23061 timestamp=87378.928 v=0
        Shelly.ListMethods via MQTT" msg-count=23062 timestamp=87379.331 v=0
        Script.Stop via MQTT" msg-count=23063 timestamp=87379.528 v=0
        shelly_notification:165 Status change of script:1: {\"id\":1,\"error_msg\":null,\"errors\":[],\"running\":false}" msg-count=23064 timestamp=87379.542 v=0

[ ] Filter out debug messages: remove messages not tight to the given script name or id
[ ] Reassemble multi-line debug message (separated by \n, not \x00) & missing leading "{" like:

        msg="\"info\": {"
        msg="\"component\": \"input:0\","
        msg="\"id\": 0," 
        msg="\"event\": \"btn_up\"," 
        msg="\"ts\": 1746978309.23000001907 }" 
        msg=}"

[x] Take into acount double-push events:

        msg="shelly_notification:211 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"double_push\",\"ts\":1746980857.36}" 

[ ] Use options.PrintResult() every where in homectl

Integration
-----------

[ ] Add support for Matter protocol for Gen2 devices via GW
[ ] Add support for Matter protocol for Gen1 devices via GW
[ ] Add GoLang profiling support es explained in <https://go.dev/blog/pprof>
[ ] BUG: Fix dpkg upgrade

        admin@myhome:~ $ sudo dpkg -i ./myhome_0.2.4_arm64.deb
        (Reading database ... 76658 files and directories currently installed.)
        Preparing to unpack ./myhome_0.2.4_arm64.deb ...
        Unpacking myhome (0.2.4) over (0.2.3) ...
        Setting up myhome (0.2.4) ...
        Failed to enable unit: Refusing to operate on alias name or linked unit file: myhome.service
        dpkg: error processing package myhome (--install):
        installed myhome package post-installation script subprocess returned error exit status 1
        Errors were encountered while processing:
        myhome

[ ] Sign dpkg package
[ ] BUG: Fix dpkg upgrade

        Failed to enable unit: Refusing to operate on alias name or linked unit file: myhome.service
        dpkg: error processing package myhome (--install):
        installed myhome package post-installation script subprocess returned error exit status 1
        Errors were encountered while processing:
        myhome

[ ] BUG: internal/ SHOULD NOT need go.mod
[ ] BUG (windows) Avoid the popup about public network to always pop-up
[ ] Build every target using matrix+go-releaser (to cache & build faster)slr268
[ ] Add GitHub actions attestation <https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds>
[x] GPG signed commits on Windows/WSL/Linux
[x] Fix error

        This version of c:\Program Files\MyHome\myhome.exe is not compatible with the version of Windows you're running. Check your computer's system information and then contact the software publisher.
        
[x] Auto-tag patch & minor increases
[x] Build Debian package on new tagged version
[x] Use goreleaser to cross-compile

        - https://goreleaser.com/ci/actions/
        - https://github.com/marketplace/actions/goreleaser-action

[ ] Build Debian package for amd64
[x] Build Debian package for arm64
[ ] Ship linux/arm64 binary in the release
[ ] Ship linux/amd64 binary in the release
[ ] Add cron-job to download (only) the binary from the latest release
[ ] Add command to auto-update Debian package installation from latest release
[ ] Package systemd scripts in-place (with stop, disable & reload as preuninstall, ans reload enable & start as postinstall)
[x] Create verified tags
[ ] Use goreleaser-cross if needed

        - https://github.com/goreleaser/goreleaser-cross

[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[ ] Sign Windows package (public key)
[x] Sign Windows package (self-signed key)
[x] Build MSI package for Windows on new tagged version
[ ] Run myhome as a windows service <https://learn.microsoft.com/en-us/troubleshoot/windows-client/setup-upgrade-and-drivers/create-user-defined-service>
[ ] Run every service under <https://github.com/kardianos/service> rather than manual packaging?
