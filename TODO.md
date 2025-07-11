TODO <!-- omit in toc -->
====

Table of Contents <!-- omit in toc -->
-----------------

- [License](#license)
- [Core Architecture](#core-architecture)
- [Bug Fixes](#bug-fixes)
- [Shelly Device Features](#shelly-device-features)
- [Monitoring \& Metrics](#monitoring--metrics)
- [Networking \& Discovery](#networking--discovery)
- [User Interface](#user-interface)
- [Integrations](#integrations)
- [Packaging \& Deployment](#packaging--deployment)
- [Code Quality](#code-quality)
- [Code Quality (continued)](#code-quality-continued)
- [Packaging \& Deployment (continued)](#packaging--deployment-continued)

License
-------

[ ] Change license from MPL-2.0 to MIT/BSD (or GPL v3) when ready & Add SPDX license identifier everywhere

Core Architecture
-----------------

[ ] Replace dependency of myhome.* on shelly.* by interfaces
[ ] Rework file/folder layout to be more generic using <https://github.com/golang-standards/project-layout>
[ ] Move homectl as ctl subcommand of myhome
[ ] Add interactive shell
    - <https://www.dolthub.com/blog/2023-03-29-interactive-shell-golang/>
    - <https://github.com/abiosoft/ishell>
[ ] Use Native slog.in-context:
    ```go
    ctx = logr.NewContext(ctx, logr.New(logr.NewJSONEncoder()))
    [...]
    log, err := logr.FromContext(ctx)
    if err != nil {
        return nil, err
    }
    ctx = slog.NewContext(ctx, slog.New(slog.NewTextHandler(os.Stdout, nil)))
    ```
[x] Use pure-Go sqlite implementation (to allow cross-compilation)
    - <https://pkg.go.dev/modernc.org/sqlite>
    - <https://github.com/ncruces/go-sqlite3>
    - <https://github.com/cvilsmeier/go-sqlite-bench>

Bug Fixes
---------

[ ] Fix new (rebooting) device not being discovered & indexed
[ ] BUG use Ip() rather than Host() to avoid an error like the below:
    ```
    {"level":"error","error":"Get \"http://[<nil>]/rpc/Shelly.GetDeviceInfo?ident=true\": dial tcp: lookup <nil>: no such host","caller":"/Users/fix/Desktop/GIT/home-automation/pkg/shelly/shttp/channel.go:37","time":1748900840030,"message":"HTTP error"}
    ```
[x] BUG shellyplugsg3 not working
[ ] BUG fix on Windows: 'Failed to install event source: Access is denied.'
[ ] BUG ZeroConf scanning (automatic resolver) not working on Windows or macOS
[ ] BUG ZeroConf scanning stops working after a while (few minutes)
[ ] BUG: Find mqtt.local. using mDNS in homectl
[ ] BUG: Fix dpkg upgrade
    ```
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
    ```
[ ] Filter out debug messages: remove messages not tight to the given script name or id
[ ] Reassemble multi-line debug messages (separated by \n, not \x00) & missing leading "{"
[x] BUG fix wrong HTTP verb
[x] BUG make homectl `forget` actually work (right now it does not seem to update the DB storage)
[x] BUG Group ID's (integers) does not increment
[x] BUG no timeout if there is no myhome instance running
[x] Fix inbound IPv6 communication
[x] HTTP POST (spurious "config" layer)
[x] Fix MQTT when several CallE() invocations are in the same run

Shelly Device Features
----------------------

[ ] Add shelly script config to homectl to manipulate CONFIG object
[ ] Overload CONFIG objects from values found as JSON-path in KVS:config/*
[ ] Deactivate Wi-Fi if Ethernet is available & active
[x] Consolidated watchdog.js (mqtt, ip-assignment, daily-reboot)
[x] Consolidated watchdog.js with Prometheus metrics endpoint
[ ] Upload watchdog.js on every device that have scripting
[ ] Configure adaptive heater control on every device that are known to be heaters (based on group membership)
[ ] Turn on/off heaters based on kalman filter and <https://developer.accuweather.com>
[ ] Create/Configure scripts in a single operation
[ ] Run simple JavaScript in a single operation
[ ] Support Matter protocol for Gen3/4 devices
[x] Disable auto-off timer of the pool-house switches (using double push)
[x] Status for one / multiple scripts in a single operation
[x] ability to change device name
[x] Check/force MQTT configuration

Monitoring & Metrics
--------------------

[x] Add Prometheus metrics endpoint for device monitoring and switch metrics
[ ] Write instructions to configure prometheus to scrape metrics from known Shelly devices
[ ] Add Grafana/Perses dashboard for Shelly devices
[ ] Add mqtt-to-syslog in myhome daemon (non broker part) to collect log messages from Shelly devices
[ ] Synchomized device names reports device name (from user) rather than device ID in Instance

Networking & Discovery
----------------------

[ ] Do not scan ZeroConf when devices are explicit
[ ] Publish myhome.local. ("penates.local."?) using mDNS
[ ] Add support for Devolo Magic2 devices, via [go-devolo-plc](https://github.com/asnowfix/go-devolo-plc>)
[x] Re-enable mDNS for early devices discovery
[x] Use ZeroConf to discover (quickly) MQTT broker
[x] Configure MQTT broker immediately after device discovery
[x] Resolve hostname using mDNS, on systems (eg. Windows) that do not have it in their system resolvers
[x] Publish mqtt.local. using mDNS
[x] Get IP addresses in the 'host' column of the 'devices' table

User Interface
-------------

[ ] Add interactive shell
    - <https://www.dolthub.com/blog/2023-03-29-interactive-shell-golang/>
    - <https://github.com/abiosoft/ishell>
[ ] Use options.PrintResult() everywhere in homectl
[x] Ctrl-C should stop myhome program (whatever the option)

Integrations
-----------

[ ] Add Amazon Alexa integration with <https://github.com/ericdaugherty/alexa-skills-kit-golang?tab=readme-ov-file>
[ ] Add Google Home integration
[ ] Add Home Assistant integration
[ ] Add support for Matter protocol for Gen2 devices via GW
[ ] Add support for Matter protocol for Gen1 devices via GW
[ ] ~~Add support for linksys velop devices (via JNAP protocol), see <https://github.com/uvjim/linksys_velop/blob/master/README.md>~~

Packaging & Deployment
----------------------

[ ] Sign dpkg package
[ ] Build MSI package for Windows on new tagged version
[ ] Run myhome as a windows service <https://learn.microsoft.com/en-us/troubleshoot/windows-client/setup-upgrade-and-drivers/create-user-defined-service>
[ ] Run every service under <https://github.com/kardianos/service> rather than manual packaging?
[x] Add GoLang profiling support as explained in <https://go.dev/blog/pprof>
[x] Re-init list of live devices at startup... or lazy version?

Code Quality
-----------

[ ] Code review by Windsurf
[ ] Auto-Stop script at upload if running
[ ] Find out proper mDNS implementation:
    - <https://medium.com/@potto_94870/understand-mdns-with-an-example-1e05ef70013b>
    - <https://andrewdupont.net/2022/01/27/using-mdns-aliases-within-your-home-network/>
    - Consider using Pion mDNS <https://github.com/pion/mdns> or HashiCorp mDNS <https://github.com/hashicorp/mdns/blob/main/server.go> rather than ZeroConf
    - <https://dave.cheney.net/2011/10/15/scratching-my-own-itch-or-how-to-publish-multicast-dns-records-in-go> and with supporting repo fork follow-up <https://github.com/ugjka/mdns>
[x] Reduce MQTT traffic (prefer using device lookup result when possible)

Code Quality (continued)
------------------------

[ ] Filter out debug messages: remove messages not tight to the given script name or id
[ ] Reassemble multi-line debug message (separated by \n, not \x00) & missing leading "{" like:

        msg="\"info\": {"
        msg="\"component\": \"input:0\","
        msg="\"id\": 0," 
        msg="\"event\": \"btn_up\"," 
        msg="\"ts\": 1746978309.23000001907 }" 
        msg=}"

[x] Take into account double-push events
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

[ ] BUG: internal/ SHOULD NOT need go.mod (remove go.work from the repo)
[ ] BUG (windows) Avoid the popup about public network to always pop-up
[x] Use pure-Go sqlite implementation (to allow cross-compilation)
    - https://pkg.go.dev/modernc.org/sqlite
    - https://github.com/ncruces/go-sqlite3
    - https://github.com/cvilsmeier/go-sqlite-bench
[x] Fix inbound IPv6 communication

Packaging & Deployment (continued)
---------------------------------

[ ] Build every target using matrix+go-releaser (to cache & build faster)
[ ] Add GitHub actions attestation <https://docs.github.com/en/actions/security-for-github-actions/using-artifact-attestations/using-artifact-attestations-to-establish-provenance-for-builds>
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

[ ] Build Debian package for amd64
[ ] Ship linux/arm64 binary in the release
[ ] Ship linux/amd64 binary in the release
[ ] Add cron-job to download (only) the binary from the latest release
[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[ ] Sign Windows package (public key)
[ ] Use goreleaser-cross if needed
    - https://github.com/goreleaser/goreleaser-cross
[x] GPG signed commits on Windows/WSL/Linux
[x] Fix error

        This version of c:\Program Files\MyHome\myhome.exe is not compatible with the version of Windows you're running. Check your computer's system information and then contact the software publisher.
        
[x] Auto-tag patch & minor increases
[x] Build Debian package on new tagged version
[x] Use goreleaser to cross-compile
    - https://goreleaser.com/ci/actions/
    - https://github.com/marketplace/actions/goreleaser-action
[x] Build Debian package for arm64
[x] Add command to auto-update Debian package installation from latest release
[x] Package systemd scripts in-place (with stop, disable & reload as preuninstall, ans reload enable & start as postinstall)
[x] Create verified tags
[x] Sign Windows package (self-signed key)
[x] Timeout on missing/non-responsive devices/server
