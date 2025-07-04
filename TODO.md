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

[ ] Fix new (rebooting) device not being discovered & indexed

        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609120341,"message":"Failed to get device by Id"}
        {"level":"info","v":0,"logger":"Mqtt#Watcher","device_id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/daemon/watch/mqtt.go:49","time":1750609120341,"message":"Device not found, creating new one"}
        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609125355,"message":"Failed to get device by Id"}
        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609125373,"message":"Failed to get device by Id"}
        {"level":"info","v":0,"logger":"Mqtt#Watcher","device_id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/daemon/watch/mqtt.go:49","time":1750609125373,"message":"Device not found, creating new one"}
        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609125384,"message":"Failed to get device by Id"}
        {"level":"info","v":0,"logger":"Mqtt#Watcher","device_id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/daemon/watch/mqtt.go:49","time":1750609125385,"message":"Device not found, creating new one"}
        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609126730,"message":"Failed to get device by Id"}
        {"level":"error","error":"sql: no rows in result set","logger":"DeviceStorage","id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/storage/db.go:158","time":1750609139158,"message":"Failed to get device by Id"}
        {"level":"info","v":0,"logger":"Mqtt#Watcher","device_id":"shelly1minig3-54320464074c","caller":"/Users/fix/Desktop/GIT/home-automation/myhome/daemon/watch/mqtt.go:49","time":1750609139158,"message":"Device not found, creating new one"}

[ ] synchomized device names reports device name (from user) rather than device ID in Instance:

{"level":"info","v":0,"logger":"DeviceManager#WatchZeroConf","device":"shellyplus1-b8d61a85ed58","caller":"/Users/fix/Desktop/GIT/home-automation/pkg/shelly/mdns.go:83","time":1748897948959,"message":"Zeroconf discovered"}
{"level":"info","v":0,"logger":"DeviceManager#WatchZeroConf","entry":{"name":"lumiere-exterieure-droite","type":"_shelly._tcp.","domain":"local.","hostname":"lumiere-exterieure-droite.local.","port":80,"text":["gen=2","app=Plus1","ver=1.6.1"],"ttl":120},"caller":"/Users/fix/Desktop/GIT/home-automation/myhome/daemon/watch/zeroconf.go:40","time":1748897948959,"message":"Browsed"}

[ ] BUG use Ip() rather than Host() to avoid an error like the below:

        {"level":"error","error":"Get \"http://[<nil>]/rpc/Shelly.GetDeviceInfo?ident=true\": dial tcp: lookup <nil>: no such host","caller":"/Users/fix/Desktop/GIT/home-automation/pkg/shelly/shttp/channel.go:37","time":1748900840030,"message":"HTTP error"}

[ ] BUG shellyplugsg3 not working

        "level":"error","error":"timeout waiting for response from shellyplugsg3-b08184a53f24 ()","logger":"mqtt","to verb":"Shelly.GetComponents","id":"shellyplugsg3-b08184a53f24","name":"","timeout":"5s","caller":"/Users/fix/Desktop/GIT/home-automation/pkg/shelly/mqtt/channel.go:56","time":1748895703248,"message":"Timeout waiting for device response"

[ ] BUG fix on Windows: 'Failed to install event source: Access is denied.'
[x] BUG fix wrong HTTP verb:

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

[x] Reduce MQTT traffic (prefer using device lookup result when possible)

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
[x] Add GoLang profiling support es explained in <https://go.dev/blog/pprof>
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
[x] Add command to auto-update Debian package installation from latest release
[x] Package systemd scripts in-place (with stop, disable & reload as preuninstall, ans reload enable & start as postinstall)
[x] Create verified tags
[ ] Use goreleaser-cross if needed

        - https://github.com/goreleaser/goreleaser-cross

[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[ ] Sign Windows package (public key)
[x] Sign Windows package (self-signed key)
[x] Build MSI package for Windows on new tagged version
[ ] Run myhome as a windows service <https://learn.microsoft.com/en-us/troubleshoot/windows-client/setup-upgrade-and-drivers/create-user-defined-service>
[ ] Run every service under <https://github.com/kardianos/service> rather than manual packaging?
[x] BUG fix crash on Rapsbian

        May 24 10:03:30 gruissan myhome[618]: panic: runtime error: invalid memory address or nil pointer dereference
        May 24 10:03:30 gruissan myhome[618]: [signal SIGSEGV: segmentation violation code=0x1 addr=0x0 pc=0x62bb04]
        May 24 10:03:30 gruissan myhome[618]: goroutine 1 [running]:
        May 24 10:03:30 gruissan myhome[618]: myhome/mqtt.MyHome({0xa94770, 0x40002ba060}, {{0xa98780?, 0x40000473e0?}, 0x40001ffd50?}, {0xa96fa0, 0x40000b8000}, {0x81e18c, 0x6}, {0x0, ...})
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/work/home-automation/home-automation/myhome/mqtt/server.go:79 +0x584
        May 24 10:03:30 gruissan myhome[618]: myhome/daemon.(*daemon).Run(0x40002ba0f0)
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/work/home-automation/home-automation/myhome/daemon/daemon.go:58 +0x1e0
        May 24 10:03:30 gruissan myhome[618]: myhome/daemon.init.func3(0xfced80?, {0x4000281f20?, 0x4?, 0x81d5ab?})
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/work/home-automation/home-automation/myhome/daemon/run.go:32 +0x13c
        May 24 10:03:30 gruissan myhome[618]: github.com/spf13/cobra.(*Command).execute(0xfced80, {0x4000281ec0, 0x3, 0x3})
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/go/pkg/mod/github.com/spf13/cobra@v1.8.1/command.go:985 +0x830
        May 24 10:03:30 gruissan myhome[618]: github.com/spf13/cobra.(*Command).ExecuteC(0xfcdf20)
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/go/pkg/mod/github.com/spf13/cobra@v1.8.1/command.go:1117 +0x344
        May 24 10:03:30 gruissan myhome[618]: github.com/spf13/cobra.(*Command).Execute(...)
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/go/pkg/mod/github.com/spf13/cobra@v1.8.1/command.go:1041
        May 24 10:03:30 gruissan myhome[618]: main.main()
        May 24 10:03:30 gruissan myhome[618]:         /home/runner/work/home-automation/home-automation/myhome/main.go:76 +0x30
        May 24 10:03:30 gruissan systemd[1]: myhome.service: Main process exited, code=exited, status=2/INVALIDARGUMENT

