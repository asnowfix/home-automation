TODO
====
[ ] Create/Configure scripts in a single operation
[x] Status for one / multiple scripts in a single operation
[ ] Run simple JavaScript in a single operation
[ ] Do not scan ZeroConf when devices are explicit
[x] HTTP POST (spurious "config" layer)
[ ] Fix MQTT when several CallE() invocations are in the same run
[ ] Add support for linksys velop devices (via JNAP protocol) 

    - https://github.com/uvjim/linksys_velop
    - https://github.com/uvjim/linksys_velop/blob/master/README.md


[ ] Rework file/folder layout to be more generic using https://github.com/golang-standards/project-layout

[ ] Publish mqtt.local. or myhome.local. hostname using mDNS, using

    - <https://medium.com/@potto_94870/understand-mdns-with-an-example-1e05ef70013b>
    - <https://andrewdupont.net/2022/01/27/using-mdns-aliases-within-your-home-network/>
    - Consider using Pion mDNS <https://github.com/pion/mdns> or HashiCorp mDNS <https://github.com/hashicorp/mdns/blob/main/server.go> rather than ZeroConf.
    - <https://dave.cheney.net/2011/10/15/scratching-my-own-itch-or-how-to-publish-multicast-dns-records-in-go> and with supporting repo fork follow-up <https://github.com/ugjka/mdns>

[ ] Add SPDX license identifier everywhere

[x] Fix error

    7:02AM INF ../mymqtt/mqtt.go:202 > Received from MQTT: payload="{\"src\":\"shellyplus1-08b61fd9333c\",\"dst\":\"shellyplus1-08b61fd9333c/events\",\"method\":\"NotifyEvent\",\"params\":{\"ts\":1736834580.08,\"events\":[{\"component\":\"sys\", \"event\":\"scheduled_restart\", \"time_ms\": 999, \"ts\":1736834580.08}]}}" topic=shellyplus1-08b61fd9333c/events/rpc v=0
    7:02AM ERR devices/db.go:98 > Failed to get device by manufacturer and ID error="sql: no rows in result set" id=shellyplus1-08b61fd9333c logger=DeviceStorage manufacturer=Shelly
    7:02AM INF devices/manager.go:161 > Device not found, creating new one device_id=shellyplus1-08b61fd9333c logger=DeviceManager v=0
    7:02AM INF ../mymqtt/mqtt.go:199 > Subscribing to: topic=NCELRND1279_shellyplus1-08b61fd9333c/rpc v=0
    7:02AM INF ../mymqtt/mqtt.go:206 > Subscribed to: topic=NCELRND1279_shellyplus1-08b61fd9333c/rpc v=0
    7:02AM INF ../pkg/shelly/ops.go:106 > Calling channel=Mqtt method_handler={"http_method":"GET","method":"Shelly.ListMethods"} out_type=*shelly.MethodsResponse params=null v=0
    7:02AM INF ../pkg/shelly/mqtt/ops.go:64 > Sending request={"id":0,"method":"Shelly.ListMethods","src":"NCELRND1279_shellyplus1-08b61fd9333c"} v=0
    7:02AM INF ../pkg/shelly/mqtt/ops.go:67 > Waiting for response v=0
    7:02AM INF ../mymqtt/mqtt.go:218 > Publishing: payload="{\"id\":0,\"src\":\"NCELRND1279_shellyplus1-08b61fd9333c\",\"method\":\"Shelly.ListMethods\"}" topic=shellyplus1-08b61fd9333c/rpc v=0
    7:02AM INF ../mymqtt/mqtt.go:220 > Published v=0
    7:02AM INF ../mymqtt/mqtt.go:202 > Received from MQTT: payload="{\"src\":\"shellyplus1-b8d61a85ed58\",\"dst\":\"shellyplus1-b8d61a85ed58/events\",\"method\":\"NotifyEvent\",\"params\":{\"ts\":1736834580.06,\"events\":[{\"component\":\"sys\", \"event\":\"scheduled_restart\", \"time_ms\": 996, \"ts\":1736834580.06}]}}" topic=shellyplus1-b8d61a85ed58/events/rpc v=0
    7:03AM INF ../mymqtt/mqtt.go:202 > Received from MQTT: payload="{\"id\":0,\"src\":\"shellyplus1-08b61fd9333c\",\"dst\":\"NCELRND1279_shellyplus1-08b61fd9333c\",\"error\":{\"code\":-109,\"message\":\"shutting down in 952 ms\"}}" topic=NCELRND1279_shellyplus1-08b61fd9333c/rpc v=0
    7:03AM INF ../pkg/shelly/mqtt/ops.go:79 > Received response={"dst":"NCELRND1279_shellyplus1-08b61fd9333c","error":{"code":-109,"message":"shutting down in 952 ms"},"id":0,"result":{"methods":null},"src":"shellyplus1-08b61fd9333c"} v=0
    7:03AM INF devices/manager.go:174 > Updating device device={"Config":"","Info":"null","Status":"","host":"<nil>","id":"shellyplus1-08b61fd9333c","manufacturer":"Shelly","name":""} logger=DeviceManager v=0
    panic: interface conversion: interface {} is []interface {}, not []mqtt.ComponentEvent

    goroutine 68 [running]:
    myhome/devices.(*Device).UpdateFromMqttEvent(0xc000222320, 0xc000394080)
            /Users/fkowalski/Desktop/Projects/home-automation/myhome/devices/device.go:125 +0x44a
    myhome/devices.(*DeviceManager).WatchMqtt.func1({0x107d2c708, 0xc00009e1e0}, {{0x0?, 0x0?}, 0x0?})
            /Users/fkowalski/Desktop/Projects/home-automation/myhome/devices/manager.go:175 +0x7e5
    created by myhome/devices.(*DeviceManager).WatchMqtt in goroutine 1
            /Users/fkowalski/Desktop/Projects/home-automation/myhome/devices/manager.go:139 +0x1c8
    exit status 2

[x] Use pure-Go sqlite implementation (to allow cross-compilation)

        - https://pkg.go.dev/modernc.org/sqlite
        - https://github.com/ncruces/go-sqlite3
        - https://github.com/cvilsmeier/go-sqlite-bench
