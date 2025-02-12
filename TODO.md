TODO
====

License
-------

[ ] Change license from MPL-2.0 to MIT/BSD when ready
[ ] Add SPDX license identifier everywhere

Functions
---------

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
[ ] Rework file/folder layout to be more generic using https://github.com/golang-standards/project-layout
[ ] Find out proper layout

    - <https://medium.com/@potto_94870/understand-mdns-with-an-example-1e05ef70013b>
    - <https://andrewdupont.net/2022/01/27/using-mdns-aliases-within-your-home-network/>
    - Consider using Pion mDNS <https://github.com/pion/mdns> or HashiCorp mDNS <https://github.com/hashicorp/mdns/blob/main/server.go> rather than ZeroConf.
    - <https://dave.cheney.net/2011/10/15/scratching-my-own-itch-or-how-to-publish-multicast-dns-records-in-go> and with supporting repo fork follow-up <https://github.com/ugjka/mdns>

[ ] Rework file/folder layout to be more generic using <https://github.com/golang-standards/project-layout>
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
[ ] Timeout on missing/non-responsive devices
[ ] Ctrl-C should stop myhome program (whatever the option)
[ ] Fix inbound IPv6 communication

Integration
-----------

[ ] Fix error

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
[ ] Create verified tags
[ ] Use goreleaser-cross if needed

        - https://github.com/goreleaser/goreleaser-cross

[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[x] Build MSI package for Windows on new tagged version
[ ] Add command to auto-update Debian package installation from latest