TODO
====

License
-------

[ ] Change license from MPL-2.0 to MIT/BSD when ready
[ ] Add SPDX license identifier everywhere

Functions
---------

[ ] Get IP addresses in the 'host' column of the 'devices' table
[ ] Create/Configure scripts in a single operation
[x] Status for one / multiple scripts in a single operation
[ ] Run simple JavaScript in a single operation
[ ] Do not scan ZeroConf when devices are explicit
[x] HTTP POST (spurious "config" layer)
[x] Fix MQTT when several CallE() invocations are in the same run
[ ] Add support for linksys velop devices (via JNAP protocol) 

    - https://github.com/uvjim/linksys_velop
    - https://github.com/uvjim/linksys_velop/blob/master/README.md

[ ] Re-enable mDNS for early devices discovery
[ ] Configure MQTT broker immediatelly after device discovery
[ ] Publish mqtt.local. or myhome.local. hostname using mDNS, using
[ ] Rework file/folder layout to be more generic using <https://github.com/golang-standards/project-layout>
[ ] Move homectl as ctl subcommand of myhome
[ ] Find out proper layout

    - <https://medium.com/@potto_94870/understand-mdns-with-an-example-1e05ef70013b>
    - <https://andrewdupont.net/2022/01/27/using-mdns-aliases-within-your-home-network/>
    - Consider using Pion mDNS <https://github.com/pion/mdns> or HashiCorp mDNS <https://github.com/hashicorp/mdns/blob/main/server.go> rather than ZeroConf.
    - <https://dave.cheney.net/2011/10/15/scratching-my-own-itch-or-how-to-publish-multicast-dns-records-in-go> and with supporting repo fork follow-up <https://github.com/ugjka/mdns>

[ ] Add Home Assistant integration
[ ] Add Amazon Alexa integration
[ ] Add Google Home integration
[ ] Add interactive shell

        - https://www.dolthub.com/blog/2023-03-29-interactive-shell-golang/
        - https://github.com/abiosoft/ishell

[x] Use pure-Go sqlite implementation (to allow cross-compilation)

        - https://pkg.go.dev/modernc.org/sqlite
        - https://github.com/ncruces/go-sqlite3
        - https://github.com/cvilsmeier/go-sqlite-bench

[ ] Re-init list of live devices at startup... or lazy version?
[x] Timeout on missing/non-responsive devices/server
[x] Ctrl-C should stop myhome program (whatever the option)
[ ] Fix inbound IPv6 communication
[x] Fix Unhandled device type device

        9:34AM ERR devices/impl/manager.go:121 > Unhandled device type device={"components":null,"config":{"ble":{"enable":true,"observer":{"enable":true},"rpc":{"enable":true}},"cloud":{"enable":true,"server":"shelly-78-eu.shelly.cloud:6022/jrpc"},"input:0":{"auto_off":false,"auto_off_delay":0,"auto_on":false,"auto_on_delay":0,"id":0,"in_mode":"","initial_state":"","name":null},"mqtt":{"client_id":"shelly1minig3-54320464f17c","enable":true,"enable_control":true,"rpc_ntf":true,"server":"192.168.1.2:1883","status_ntf":true,"topic_prefix":"shelly1minig3-54320464f17c","use_client_cert":false},"switch:0":{"auto_off":false,"auto_off_delay":60,"auto_on":false,"auto_on_delay":60,"id":0,"in_mode":"follow","initial_state":"off","name":"Lumière Escalier Exterieur"},"system":{"cfg_rev":23,"debug":{"mqtt":{"enable":false},"udp":{"enable":false},"websocket":{"enable":false}},"device":{"discoverable":true,"eco_mode":false,"fw_id":"20231121-110944/1.1.99-minig3prod1-ga898543","mac":"54320464F17C","name":"light-outside-steps","profile":""},"location":{"lat":43.6611,"lon":6.9808,"tz":"Europe/Paris"},"rpc_udp":{"dst_addr":{"IP":"","Zone":""}},"sntp":{"server":"time.google.com"}},"wifi":{"ap":{"password":"","ssid":"Shelly1MiniG3-54320464F17C"},"mode":"","password":"","ssid":"","sta":{"password":"","ssid":"Shelly1MiniG3-54320464A1D0"},"sta1":{"password":"","ssid":"Linksys_7A50-guest"}},"ws":{"enable":false,"server":null,"ssl_ca":"ca.pem"}},"config_revision":0,"host":"shelly1minig3-54320464f17c.local.","id":"shelly1minig3-54320464f17c","info":{"app":"Mini1G3","auth_en":false,"discoverable":false,"fw_id":"20231121-110944/1.1.99-minig3prod1-ga898543","gen":3,"id":"shelly1minig3-54320464f17c","mac":"54320464F17C","model":"S3SW-001X8EU","ver":"1.1.99-minig3prod1"},"mac":"e7:8d:f6:d3:8e:b8:17:5e:c2","manufacturer":"Shelly","name":"light-outside-steps","status":{"ble":{},"cloud":{"connected":true},"input:0":{"id":0,"state":false},"mqtt":{"connected":true},"switch:0":{"aenergy":{"by_minute":null,"minute_ts":0,"total":0},"errors":null,"freq":0,"id":0,"input":{"id":0,"state":false},"output":false,"pf":0,"source":"loopback","temperature":{"tC":41.1,"tF":106}},"wifi":{"ip":"","ssid":"Shelly1MiniG3-54320464A1D0","strength":0},"ws":{"connected":false}}} logger=DeviceManager/DeviceManager#DeviceChannel type=null
        
 [ ] Fix timeout on MQTT (always on Shelly.GetComponents)

        12:10AM ERR ../pkg/shelly/mqtt/channel.go:54 > Timeout waiting for response from device=shellypro1-30c6f782d274 logger=mqtt timeout=5s to verb=Shelly.GetComponents

[ ] Fix duplicate MAC address issue

        10:59PM INF ../pkg/shelly/device.go:267 > Shelly.init host="marshaling error: json: unsupported type: func() string" id=shellyplus1-08b61fd141e8 logger=Mqtt#Watcher v=0
        10:59PM INF ../pkg/shelly/device.go:325 > Shelly.methods host="marshaling error: json: unsupported type: func() string" id=shellyplus1-08b61fd141e8 logger=Mqtt#Watcher v=0
        10:59PM INF ../pkg/shelly/ops.go:169 > Calling channel=mqtt out_type=*shelly.MethodsResponse params=null v=0
        10:59PM INF ../pkg/shelly/ops.go:169 > Calling channel=mqtt out_type=*system.Status params={"available_updates":{},"cfg_rev":0,"fs_free":0,"fs_size":0,"kvs_rev":0,"mac":null,"ram_free":0,"ram_size":0,"reset_reason":0,"restart_required":false,"schedule_rev":0,"time":"","unixtime":0,"uptime":0,"webhook_rev":0} v=0
        10:59PM INF ../internal/myhome/device.go:185 > Updated device device={"components":null,"config":{"system":{"cfg_rev":0,"debug":{"mqtt":{"enable":false},"udp":{"enable":false},"websocket":{"enable":false}},"device":{"discoverable":false,"eco_mode":false,"fw_id":"","mac":null,"name":"","profile":""},"location":{},"rpc_udp":{"dst_addr":{"IP":"","Zone":""}},"sntp":{"server":""}}},"config_revision":0,"host":".local.","id":"","info":{"auth_en":false,"discoverable":false,"fw_id":"","id":""},"mac":"d3:c0:7a:d4:50:f5:e3:51:3c","manufacturer":"Shelly","name":"","status":{"mqtt":{"connected":true},"system":{"available_updates":{},"cfg_rev":0,"fs_free":0,"fs_size":0,"kvs_rev":0,"mac":null,"ram_free":0,"ram_size":0,"reset_reason":0,"restart_required":false,"schedule_rev":0,"time":"","unixtime":0,"uptime":0,"webhook_rev":0}}} logger=Mqtt#Watcher v=0
        10:59PM INF devices/cache.go:63 > inserted/updated device id= logger=Cache name= v=0
        10:59PM ERR storage/db.go:129 > Failed to upsert device error="sqlite3: constraint failed: UNIQUE constraint failed: devices.mac" device={"components":null,"config":{"system":{"cfg_rev":0,"debug":{"mqtt":{"enable":false},"udp":{"enable":false},"websocket":{"enable":false}},"device":{"discoverable":false,"eco_mode":false,"fw_id":"","mac":null,"name":"","profile":""},"location":{},"rpc_udp":{"dst_addr":{"IP":"","Zone":""}},"sntp":{"server":""}}},"config_revision":0,"host":".local.","id":"","info":{"auth_en":false,"discoverable":false,"fw_id":"","id":""},"mac":"d3:c0:7a:d4:50:f5:e3:51:3c","manufacturer":"Shelly","name":"","status":{"mqtt":{"connected":true},"system":{"available_updates":{},"cfg_rev":0,"fs_free":0,"fs_size":0,"kvs_rev":0,"mac":null,"ram_free":0,"ram_size":0,"reset_reason":0,"restart_required":false,"schedule_rev":0,"time":"","unixtime":0,"uptime":0,"webhook_rev":0}}} logger=DeviceStorage
        10:59PM ERR devices/impl/manager.go:126 > Failed to set device error="sqlite3: constraint failed: UNIQUE constraint failed: devices.mac" device={"components":null,"config":{"system":{"cfg_rev":0,"debug":{"mqtt":{"enable":false},"udp":{"enable":false},"websocket":{"enable":false}},"device":{"discoverable":false,"eco_mode":false,"fw_id":"","mac":null,"name":"","profile":""},"location":{},"rpc_udp":{"dst_addr":{"IP":"","Zone":""}},"sntp":{"server":""}}},"config_revision":0,"host":".local.","id":"","info":{"auth_en":false,"discoverable":false,"fw_id":"","id":""},"mac":"d3:c0:7a:d4:50:f5:e3:51:3c","manufacturer":"Shelly","name":"","status":{"mqtt":{"connected":true},"system":{"available_updates":{},"cfg_rev":0,"fs_free":0,"fs_size":0,"kvs_rev":0,"mac":null,"ram_free":0,"ram_size":0,"reset_reason":0,"restart_required":false,"schedule_rev":0,"time":"","unixtime":0,"uptime":0,"webhook_rev":0}}} logger=DeviceManager

Integration
-----------

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
[ ] Add cron-job to download the binary from the latest release
[ ] Package systemd scripts in-place (with stop, disable & reload as preuninstall, ans reload enable & start as postinstall)
[x] Create verified tags
[ ] Use goreleaser-cross if needed

        - https://github.com/goreleaser/goreleaser-cross

[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[x] Build MSI package for Windows on new tagged version
[ ] Add command to auto-update Debian package installation from latest release
[ ] Run myhome as a windows service <https://learn.microsoft.com/en-us/troubleshoot/windows-client/setup-upgrade-and-drivers/create-user-defined-service>
