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

[ ] BUG ZeroConf scanning not working on Windows
[ ] Support matter protocol for Gen3/4 devices
[x] BUG Group ID's (integers) do not increment
[x] BUG no timeout if there is no myhome instance running
[ ] BUG ability to change device name
[x] Check/force MQTT configuration
[x] Get IP addresses in the 'host' column of the 'devices' table
[ ] Create/Configure scripts in a single operation
[x] Status for one / multiple scripts in a single operation
[ ] Run simple JavaScript in a single operation
[ ] Do not scan ZeroConf when devices are explicit
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

Integration
-----------

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
[ ] Add cron-job to download the binary from the latest release
[ ] Add command to auto-update Debian package installation from latest release
[ ] Package systemd scripts in-place (with stop, disable & reload as preuninstall, ans reload enable & start as postinstall)
[x] Create verified tags
[ ] Use goreleaser-cross if needed

        - https://github.com/goreleaser/goreleaser-cross

[ ] Build Debian package the official way using <https://github.com/marketplace/actions/build-debian-packages>
[x] Sign Windows package (public key°)
[x] Sign Windows package (self-signed key°)
[x] Build MSI package for Windows on new tagged version
[ ] Run myhome as a windows service <https://learn.microsoft.com/en-us/troubleshoot/windows-client/setup-upgrade-and-drivers/create-user-defined-service>
