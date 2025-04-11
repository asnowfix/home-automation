# MyHome - Penates

## Abstract

MyHome Penates is the home automation system I develop & use to control my house.  This is a hobby project to learn Go.  I use mostly using (very cool) Shelly devices, from Alterco Robotics.

## Table of Contents <!-- omit in toc -->

- [Home Automation](#home-automation)
  - [Abstract](#abstract)
  - [Usage - Linux](#usage---linux)
    - [Is daemon running?](#is-daemon-running)
    - [Manual start](#manual-start)
  - [Usage Windows](#usage-windows)
  - [Shelly Notes](#shelly-notes)
    - [Shelly 1 H\&T](#shelly-1-ht)
    - [Web-Sockets Logs](#web-sockets-logs)
    - [Shelly MQTT Notes](#shelly-mqtt-notes)
      - [Any topic](#any-topic)
      - [Shelly H\&T Gen1 (FIXME)](#shelly-ht-gen1-fixme)
      - [Test MQTT CLI](#test-mqtt-cli)
      - [Shelly H\&T Gen1](#shelly-ht-gen1)
  - [GCP Notes](#gcp-notes)
  - [Shelly Devices](#shelly-devices)
    - [Gen 3](#gen-3)
    - [Gen 2](#gen-2)
      - [Pro1 - Gen 2](#pro1---gen-2)
      - [Plus1 - Gen2](#plus1---gen2)
  - [Red-by-SFR Box Notes](#red-by-sfr-box-notes)
    - [Main API](#main-api)
    - [UPnP](#upnp)
  - [References](#references)

## Releases

Published here: <https://github.com/asnowfix/home-automation/releases>.

## Usage - Linux

### Is daemon running?

```shell
$ systemctl status myhome@fix.service
myhome@fix.service - MyHome as a system service
     Loaded: loaded (/etc/systemd/system/myhome@.service; enabled; vendor preset: enabled)
     Active: activating (auto-restart) (Result: exit-code) since Wed 2024-05-01 10:23:50 CEST; 1s ago
    Process: 3150933 ExecStart=/usr/bin/env /home/fix/go/bin/myhome -v (code=exited, status=127)
   Main PID: 3150933 (code=exited, status=127)
```

### Manual start

```bash
make start
```

## Usage Windows

Unless you suceed to set `$env:Path` in pwsh, you need to call GNU Make with its full Path.

```bash
C:\ProgramData\chocolatey\bin\make build
```

## Groups of devices

Groups are collections of devices that can be controlled together.

### Create a group

```shell
group create radiateurs switched-off=on
```

The optional key-value pair `switched-off=on` indicates that Shelly switch devices controlling the _radiateurs_ need to be turned `on` for the _radiateurs_ to be turned `off`.

### Add a devices to a group

By device name:

```shell
group add radiateurs radiateur-bureau
```

...or by IP:

```shell
group add radiateurs 192.168.1.37
```

...or by device Id:

```shell
group add radiateurs shelly1minig3-84fce63bf464
```

### List groups

```shell
group list
```
```yaml
groups:
    - id: 2
      name: radiateurs
      kvs: '{"switched-off":"on"}'
```

### Show a group

```shell
group show radiateurs
```
```yaml
groupinfo:
    id: 2
    name: radiateurs
    kvs: '{"switched-off":"on"}'
devices:
    - deviceidentifier:
        manufacturer: Shelly
        id_: shellyplus1-b8d61a85a970
      mac: 07:c0:fa:d4:0f:39:03:de:f4
      host: 192.168.1.78
      name_: radiateur-bureau
```

### Delete a group

```shell
group delete radiateurs
```

## Shelly Notes

```
http://192.168.33.1/rpc/HTTP.GET?url="http://admin:supersecretpassword@10.33.53.21/rpc/Shelly.Reboot"
```

### Shelly 1 H&T

URL update to sensor API:

```
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 url: /?hum=89&temp=9.88&id=shellyht-EE45E9
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 header: Content-Length: [0]
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 header: User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]
févr. 15 22:04:09 palmbeach env[191666]: 2024/02/15 22:04:09 body:
```

Same as:

```
$ curl -X POST -H 'User-Agent: [Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)]' 'http://192.168.1.2:8888/?hum=89&temp=9.88&id=shellyht-
EE45E9'
```

Test output

```
go install
sudo systemctl stop myhome@fix.service
sudo systemctl start myhome@fix.service
systemctl status myhome@fix.service
```

### Web-Sockets Logs

From <https://shelly-api-docs.shelly.cloud/gen2/Scripts/Tutorial>:


```bash
export SHELLY=192.168.1.39
curl -X POST -d '{"id":1, "method":"Sys.SetConfig","params":{"config":{"debug":{"websocket":{"enable":true}}}}}' http://${SHELLY}/rpc
wscat --connect ws://${SHELLY}/debug/log
```
```log
< {"ts":1733774548.629, "level":2, "data":"shelly_debug.cpp:236    Streaming logs to 192.168.1.2:40234", "fd":1}
< {"ts":1733774573.492, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774573.494, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774573.497, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774573.499, "level":2, "data":"    \"state\": true, \"ts\": 1733774573.41000008583 }", "fd":102}
< {"ts":1733774573.501, "level":2, "data":" }", "fd":102}
< {"ts":1733774573.503, "level":2, "data":"Toggle lustre light", "fd":102}
< {"ts":1733774573.505, "level":2, "data":"shelly_ejs_rpc.cpp:41   Shelly.call HTTP.POST {\"url\":\"http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle\",\"body\":\"{\\\"id\\\":0}\"}", "fd":1}
< {"ts":1733774573.508, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":true}", "fd":1}
< {"ts":1733774573.543, "level":2, "data":"shos_rpc_inst.c:243     HTTP.POST via loopback ", "fd":1}
< {"ts":1733774573.547, "level":2, "data":"shelly_http_client.:308 0x3ffe4998: HTTP POST http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle", "fd":1}
< {"ts":1733774573.670, "level":2, "data":"  \"id\": 0, \"now\": 1733774573.60793089866, ", "fd":102}
< {"ts":1733774573.672, "level":2, "data":"  \"info\": { ", "fd":102}
< {"ts":1733774573.674, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774573.676, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774573.678, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774573.680, "level":2, "data":"    \"state\": false, \"ts\": 1733774573.60999989509 }", "fd":102}
< {"ts":1733774573.682, "level":2, "data":" }", "fd":102}
< {"ts":1733774573.684, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":false}", "fd":1}
< {"ts":1733774573.751, "level":2, "data":"shelly_http_client.:611 0x3ffe4998: Finished; bytes 132, code 200, redir 0/3, auth 0, status OK", "fd":1}
< {"ts":1733774574.909, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774574.912, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774574.913, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774574.915, "level":2, "data":"    \"state\": true, \"ts\": 1733774574.82999992370 }", "fd":102}
< {"ts":1733774574.917, "level":2, "data":" }", "fd":102}
< {"ts":1733774574.919, "level":2, "data":"Toggle lustre light", "fd":102}
< {"ts":1733774574.921, "level":2, "data":"shelly_ejs_rpc.cpp:41   Shelly.call HTTP.POST {\"url\":\"http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle\",\"body\":\"{\\\"id\\\":0}\"}", "fd":1}
< {"ts":1733774574.925, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":true}", "fd":1}
< {"ts":1733774574.973, "level":2, "data":"shos_rpc_inst.c:243     HTTP.POST via loopback ", "fd":1}
< {"ts":1733774574.977, "level":2, "data":"shelly_http_client.:308 0x3ffe4a0c: HTTP POST http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle", "fd":1}
< {"ts":1733774574.980, "level":2, "data":"shos_init.c:94          New min heap free: 107092", "fd":1}
< {"ts":1733774574.982, "level":2, "data":"shos_init.c:94          New min heap free: 106164", "fd":1}
< {"ts":1733774574.996, "level":2, "data":"shos_init.c:94          New min heap free: 105400", "fd":1}
< {"ts":1733774575.020, "level":2, "data":"shelly_http_client.:611 0x3ffe4a0c: Finished; bytes 131, code 200, redir 0/3, auth 0, status OK", "fd":1}
< {"ts":1733774575.109, "level":2, "data":"  \"id\": 0, \"now\": 1733774575.04737496376, ", "fd":102}
< {"ts":1733774575.112, "level":2, "data":"  \"info\": { ", "fd":102}
< {"ts":1733774575.114, "level":2, "data":"    \"component\": \"input:0\", ", "fd":102}
< {"ts":1733774575.116, "level":2, "data":"    \"id\": 0, ", "fd":102}
< {"ts":1733774575.117, "level":2, "data":"    \"event\": \"toggle\", ", "fd":102}
< {"ts":1733774575.119, "level":2, "data":"    \"state\": false, \"ts\": 1733774575.04999995231 }", "fd":102}
< {"ts":1733774575.121, "level":2, "data":" }", "fd":102}
< {"ts":1733774575.123, "level":2, "data":"shelly_notification:162 Status change of input:0: {\"id\":0,\"state\":false}", "fd":1}
< {"ts":1733774575.144, "level":2, "data":"shos_init.c:94          New min heap free: 104656", "fd":1}
< {"ts":1733774584.700, "level":2, "data":"shelly_debug.cpp:149    Stopped streaming logs to 192.168.1.57:53127", "fd":1}
```

### Shelly MQTT Notes

- [Hive MQTT CLI Installation](https://hivemq.github.io/mqtt-cli/docs/installation/)

#### Any topic

```bash
mqtt sub -d -t '#' -h 192.168.1.2
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctblbp0vpopou2bqq0t0, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=#, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('true')
    MqttPublish{topic=shelly1minig3-54320464f17c/online, payload=4byte, qos=AT_LEAST_ONCE, retain=true}
true
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776884.28,"input:3":{"id":3,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776884.28,"input:3":{"id":3,"state":true}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"id":3,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:3, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":3,"state":false}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776888.34,"input:3":{"id":3,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733776888.34,"input:3":{"id":3,"state":true}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"src":"shelly1minig3-54320464f17c","dst":"shelly1minig3-54320464f17c/events","method":"NotifyStatus","params":{"ts":1733776888.48,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}')
    MqttPublish{topic=shelly1minig3-54320464f17c/events/rpc, payload=186byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shelly1minig3-54320464f17c","dst":"shelly1minig3-54320464f17c/events","method":"NotifyStatus","params":{"ts":1733776888.48,"switch:0":{"id":0,"output":false,"source":"HTTP_in"}}}
[...]
Client 'ctblbp0vpopou2bqq0t0@192.168.1.2' received PUBLISH ('{"id":3,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:3, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":3,"state":false}
[...]
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('shellyplusi4-c4d8d554ad6c 427 1733777582.752 1|shos_dns_sd_respond:236 ws(0x3ffde77c): Announced ShellyPlusI4-C4D8D554AD6C any@any (192.168.1.39)')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/debug/log, payload=145byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
[...]
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":0,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:0, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":0,"state":false}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=57}
```

Click & Release Button 2

```log
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.79,"input:2":{"id":2,"state":true}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=162byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.79,"input:2":{"id":2,"state":true}}}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=58}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":2,"state":true}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:2, payload=21byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":2,"state":true}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=59}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.98,"input:2":{"id":2,"state":false}}}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/events/rpc, payload=163byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"src":"shellyplusi4-c4d8d554ad6c","dst":"shellyplusi4-c4d8d554ad6c/events","method":"NotifyStatus","params":{"ts":1733777785.98,"input:2":{"id":2,"state":false}}}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=60}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":2,"state":false}')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/input:2, payload=22byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":2,"state":false}
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' sending PUBACK
    MqttPubAck{reasonCode=SUCCESS, packetIdentifier=61}
```

#### Shelly H&T Gen1 (FIXME)

Debug log:

```log
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:23 > header: %s: %s Content-Length=["0"] v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:23 > header: %s: %s User-Agent=["Shelly/20230913-112531/v1.14.0-gcb84623 (SHHT-1)"] v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:44 > http.HandleFunc url=/?hum=69&temp=17.62&id=shellyht-208500 v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:46 > http.HandleFunc query={"hum":["69"],"id":["shellyht-208500"],"temp":["17.62"]} v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:68 > http.HandleFunc gen1_device={"humidity":69,"ip":"192.168.1.37"} v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/http/server.go:72 > http.HandleFunc gen1_json="{\"ip\":\"192.168.1.37\",\"humidity\":69}" v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/pkg/shelly/gen1/publisher.go:36 > gen1.Publisher: MQTT(%v) <<< %v shellyht-208500/events/rpc="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:13 > logs.Waiter: topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:29 > logs.Waiter: already known topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/mymqtt/mqtt.go:211 > MqttSubscribe received: payload="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" topic=shellyht-208500/events/rpc v=0
déc. 09 21:51:13 palmbeach myhome[609413]: 9:51PM INF ../../../Desktop/GIT/home-automation/myhome/logs/waiter.go:25 > logs.Waiter payload="{\"id\":0,\"tC\":17.62,\"tF\":63.716003}" topic=shellyht-208500/events/rpc v=0
```

MQTT log:

```log
Client 'ctblgrgvpopou2bqq0tg@192.168.1.2' received PUBLISH ('{"id":0, "source":"HTTP_in", "output":false,"temperature":{"tC":40.5, "tF":104.9}}')
    MqttPublish{topic=shelly1minig3-54320464f17c/status/switch:0, payload=82byte, qos=AT_LEAST_ONCE, retain=false, messageExpiryInterval=86400}
{"id":0, "source":"HTTP_in", "output":false,"temperature":{"tC":40.5, "tF":104.9}}
```

#### Test MQTT CLI

```bash
mqtt sub -d -t shellyplusi4-c4d8d554ad6c/status/3 -h 192.168.1.2
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctbl8t0vpopou2bqq0r0, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=shellyplusi4-c4d8d554ad6c/status/3, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
[...]
Client 'ctbl8t0vpopou2bqq0r0@192.168.1.2' received PUBLISH ('bar')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false, messageExpiryInterval=86400}
bar
```

```bash
mqtt pub --topic=shellyplusi4-c4d8d554ad6c/status/3 -m="bar" --host=192.168.1.2 --debug
```
```log
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=ctbla00vpopou2bqq0sg, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'ctbla00vpopou2bqq0sg@192.168.1.2' sending PUBLISH ('bar')
    MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false}
Client 'ctbla00vpopou2bqq0sg@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=shellyplusi4-c4d8d554ad6c/status/3, payload=3byte, qos=AT_MOST_ONCE, retain=false}}
```


#### Shelly H&T Gen1

Subscribe to Shelly H&T Gen1:

```log
$ mqtt sub -d -t shellyht-EE45E9/events/rpc -h 192.168.1.2
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=cnfrgl0vpopiu8vsbo1g, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'cnfrgl0vpopiu8vsbo1g@192.168.1.2' sending SUBSCRIBE
    MqttSubscribe{subscriptions=[MqttSubscription{topicFilter=shellyht-EE45E9/events/rpc, qos=EXACTLY_ONCE, noLocal=false, retainHandling=SEND, retainAsPublished=false}]}
Client 'cnfrgl0vpopiu8vsbo1g@192.168.1.2' received SUBACK
    MqttSubAck{reasonCodes=[GRANTED_QOS_2], packetIdentifier=65526}
```

```log
$ mqtt pub --topic=foo -m="bar" --host=192.168.1.2 --debug
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=csoekd0vpoph78legnfg, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'csoekd0vpoph78legnfg@192.168.1.2' sending PUBLISH ('bar')
    MqttPublish{topic=foo, payload=3byte, qos=AT_MOST_ONCE, retain=false}
Client 'csoekd0vpoph78legnfg@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=foo, payload=3byte, qos=AT_MOST_ONCE, retain=false}}
```

Publish to Shelly H&T Gen1:

```shell
$ mqtt pub -d -t shellyht-EE45E9/events/rpc -h 192.168.1.2 -m '{"a":"b"}'
Client 'UNKNOWN@192.168.1.2' sending CONNECT
    MqttConnect{keepAlive=60, cleanStart=true, sessionExpiryInterval=0}
Client 'UNKNOWN@192.168.1.2' received CONNACK
    MqttConnAck{reasonCode=SUCCESS, sessionPresent=false, assignedClientIdentifier=cngepjovpopiu8vsbo20, restrictions=MqttConnAckRestrictions{receiveMaximum=1024, maximumPacketSize=268435460, topicAliasMaximum=0, maximumQos=EXACTLY_ONCE, retainAvailable=true, wildcardSubscriptionAvailable=true, sharedSubscriptionAvailable=true, subscriptionIdentifiersAvailable=true}}
Client 'cngepjovpopiu8vsbo20@192.168.1.2' sending PUBLISH ('{"a":"b"}')
    MqttPublish{topic=shellyht-EE45E9/events/rpc, payload=9byte, qos=AT_MOST_ONCE, retain=false}
Client 'cngepjovpopiu8vsbo20@192.168.1.2' finish PUBLISH
    MqttPublishResult{publish=MqttPublish{topic=shellyht-EE45E9/events/rpc, payload=9byte, qos=AT_MOST_ONCE, retain=false}}
```

## GCP Notes

```shell
$ gcloud compute project-info describe --project "homeautomation-402816"
commonInstanceMetadata:
  fingerprint: dZXOiHlTSW8=
  kind: compute#metadata
creationTimestamp: '2023-11-01T02:10:02.993-07:00'
defaultNetworkTier: PREMIUM
defaultServiceAccount: 313423816598-compute@developer.gserviceaccount.com
id: '4099453077804788485'
kind: compute#project
name: homeautomation-402816
quotas:
- limit: 1000.0
  metric: SNAPSHOTS
  usage: 0.0
[...]
```

```shell
cd myzone
go run .
tonnara:myzone fix$ go run .
panic: googleapi: got HTTP response code 404 with body: <!DOCTYPE html>
<html lang=en>
  <meta charset=utf-8>
  <meta name=viewport content="initial-scale=1, minimum-scale=1, width=device-width">
  <title>Error 404 (Not Found)!!1</title>
  <style>
   [...]
  </style>
  <a href=//www.google.com/><span id=logo aria-label=Google></span></a>
  <p><b>404.</b> <ins>That’s an error.</ins>
  <p>The requested URL <code>/dns/v2/projects/homeautomation-402816/locations/europe-west9/managedZones</code> was not found on this server.  <ins>That’s all we know.</ins>
```

See <https://cloud.google.com/sdk/gcloud/reference/dns/managed-zones/list>

```shell
$ go run .
panic: googleapi: Error 401: API keys are not supported by this API. Expected OAuth2 access token or other authentication credentials that assert a principal. See https://cloud.google.com/docs/authentication
Details:
[
  {
    "@type": "type.googleapis.com/google.rpc.ErrorInfo",
    "domain": "googleapis.com",
    "metadata": {
      "method": "cloud.dns.api.v2.ManagedZonesService.List",
      "service": "dns.googleapis.com"
    },
    "reason": "CREDENTIALS_MISSING"
  }
]

More details:
Reason: required, Message: Login Required.
```

## Shelly Devices

### Gen 3

### Gen 2

#### Pro1 - Gen 2

```json
{
  "model": "ShellyPro1",
  "mac": "30C6F782D274",
  "app": "Pro1",
  "ver": "1.0.8",
  "gen": 2,  "service": "shellypro1-30c6f782d274._shelly._tcp.local.",
  "host": "ShellyPro1-30C6F782D274.local.",
  "ipv4": "192.168.1.60",
  // ...
}
```

#### Plus1 - Gen2

```json
{
  "model": "ShellyPlus1",
  "mac": "08B61FD141E8",
  "app": "Plus1",
  "ver": "1.0.8",
  "gen": 2,
  "service": "shellyplus1-08b61fd141e8._shelly._tcp.local.",
  "host": "ShellyPlus1-08B61FD141E8.local.",
  "ipv4": "192.168.1.76",
  "port": 80,
  // ...
}
```

```shell
$ curl -s http://ShellyPlus1-4855199C9888.local/rpc/Switch.GetStatus?id=0 | jq
{
  "id": 0,
  "source": "init",
  "output": true,
  "temperature": {
    "tC": 52.4,
    "tF": 126.3
  }
}
```

```shell
$ curl -s http://ShellyPlus1-4855199C9888.local/rpc/Switch.GetConfig?id=0 | jq
{
  "id": 0,
  "name": "Development",
  "in_mode": "follow",
  "initial_state": "on",
  "auto_on": false,
  "auto_on_delay": 60,
  "auto_off": false,
  "auto_off_delay": 1
}
```

## Red-by-SFR Box Notes

### Main API

```shell
$ curl -s -G  http://192.168.1.1/api/1.0/?method=auth.getToken
<?xml version="1.0" encoding="UTF-8"?>
<rsp stat="ok" version="1.0">
     <auth token="665ae99c7ff692d186fdca08ba2a8c" method="all" />
</rsp>
```

### UPnP

```shell
$ sudo apt install xmlstarlet gupnp-tools
$ cat /proc/net/route | awk '{if($2=="00000000"){print $1}else{next}}'
enp1s0
$ gssdp-discover -i enp1s0 --timeout=3
[...]
resource available
  USN:      uuid:a6863339-b260-4d65-a9ac-6b73204d56f4::urn:neufboxtv-org:service:Resources:1
  Location: http://192.168.1.28:49153/uuid:7caa1f0b-ea52-485a-bd1d-5fe9ff0da2df/description.xml
[...]
resource available
  USN:      uuid:a04bed62-57f7-4885-91cc-e44e321a3ca7::urn:schemas-upnp-org:device:WANConnectionDevice:1
  Location: http://192.168.1.1:49152/rootDesc.xml
[...]
$ curl http://192.168.1.1:49152/rootDesc.xml | xmlstarlet fo
```
```xml
<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <specVersion>
    <major>1</major>
    <minor>0</minor>
  </specVersion>
  <device>
    <deviceType>urn:schemas-upnp-org:device:InternetGatewayDevice:1</deviceType>
    <friendlyName>neufbox router</friendlyName>
    <manufacturer>neufbox</manufacturer>
    <manufacturerURL>http://efixo.com</manufacturerURL>
    <modelDescription>neufbox router</modelDescription>
    <modelName>neufbox router</modelName>
    <modelNumber>1</modelNumber>
    <modelURL>http://efixo.com</modelURL>
    <serialNumber>00000000</serialNumber>
    <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca5</UDN>
    <serviceList>
      <service>
        <serviceType>urn:schemas-upnp-org:service:Layer3Forwarding:1</serviceType>
        <serviceId>urn:upnp-org:serviceId:Layer3Forwarding1</serviceId>
        <controlURL>/ctl/L3F</controlURL>
        <eventSubURL>/evt/L3F</eventSubURL>
        <SCPDURL>/L3F.xml</SCPDURL>
      </service>
    </serviceList>
    <deviceList>
      <device>
        <deviceType>urn:schemas-upnp-org:device:WANDevice:1</deviceType>
        <friendlyName>WANDevice</friendlyName>
        <manufacturer>MiniUPnP</manufacturer>
        <manufacturerURL>http://miniupnp.free.fr/</manufacturerURL>
        <modelDescription>WAN Device</modelDescription>
        <modelName>WAN Device</modelName>
        <modelNumber>20220123</modelNumber>
        <modelURL>http://miniupnp.free.fr/</modelURL>
        <serialNumber>00000000</serialNumber>
        <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca6</UDN>
        <UPC>000000000000</UPC>
        <serviceList>
          <service>
            <serviceType>urn:schemas-upnp-org:service:WANCommonInterfaceConfig:1</serviceType>
            <serviceId>urn:upnp-org:serviceId:WANCommonIFC1</serviceId>
            <controlURL>/ctl/CmnIfCfg</controlURL>
            <eventSubURL>/evt/CmnIfCfg</eventSubURL>
            <SCPDURL>/WANCfg.xml</SCPDURL>
          </service>
        </serviceList>
        <deviceList>
          <device>
            <deviceType>urn:schemas-upnp-org:device:WANConnectionDevice:1</deviceType>
            <friendlyName>WANConnectionDevice</friendlyName>
            <manufacturer>MiniUPnP</manufacturer>
            <manufacturerURL>http://miniupnp.free.fr/</manufacturerURL>
            <modelDescription>MiniUPnP daemon</modelDescription>
            <modelName>MiniUPnPd</modelName>
            <modelNumber>20220123</modelNumber>
            <modelURL>http://miniupnp.free.fr/</modelURL>
            <serialNumber>00000000</serialNumber>
            <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca7</UDN>
            <UPC>000000000000</UPC>
            <serviceList>
              <service>
                <serviceType>urn:schemas-upnp-org:service:WANIPConnection:1</serviceType>
                <serviceId>urn:upnp-org:serviceId:WANIPConn1</serviceId>
                <controlURL>/ctl/IPConn</controlURL>
                <eventSubURL>/evt/IPConn</eventSubURL>
                <SCPDURL>/WANIPCn.xml</SCPDURL>
              </service>
            </serviceList>
          </device>
        </deviceList>
      </device>
      <device>
        <deviceType>urn:schemas-upnp-org:device:EventDevice:1</deviceType>
        <friendlyName>NeufboxEventDevice</friendlyName>
        <manufacturer>efixo</manufacturer>
        <manufacturerURL>http://www.efixo.com/</manufacturerURL>
        <modelDescription>software Event Device</modelDescription>
        <modelName>Neufbox Event Device</modelName>
        <modelNumber>20220123</modelNumber>
        <modelURL>http://www.efixo.com/</modelURL>
        <serialNumber>00000000</serialNumber>
        <UDN>uuid:a04bed62-57f7-4885-91cc-e44e321a3ca8</UDN>
        <UPC>000000000000</UPC>
        <serviceList>
          <service>
            <serviceType>urn:neufbox-org:service:NeufBoxEvent:1</serviceType>
            <serviceId>urn:neufbox-org:serviceId:NeufBoxEvent1</serviceId>
            <controlURL>/ctl/NBX</controlURL>
            <eventSubURL>/evt/NBX</eventSubURL>
            <SCPDURL>/NBX.xml</SCPDURL>
          </service>
        </serviceList>
      </device>
    </deviceList>
    <presentationURL>http://192.168.1.1/</presentationURL>
  </device>
</root>```

## Mochi-MQTT Notes

```shell
$ go get github.com/mochi-mqtt/server/v2
```

### Port reserved by SFR-Box

These ports are not usable for NAT > Port Redirection.

```
1287/tcp
1288/tcp
1290-1339/tcp
2427/udp
5060/both
35500-35599/udp
68/udp
8254/udp
64035-65535/both
9/udp
5086/tcp
15086/udp
```

## References

1. Google Cloud
   1. <https://cloud.google.com/dns?hl=en>
   2. <https://cloud.google.com/dns/docs/registrars>
   3. <https://cloud.google.com/api-gateway/docs/reference/rest/v1/projects.locations>
   4. <https://pkg.go.dev/google.golang.org/api>
   5. <https://dcc.godaddy.com/manage/asnowfix.fr/dns>
   6. <https://console.cloud.google.com/net-services/dns/zones/asnowfix-root/details?project=homeautomation-402816>
   7. <https://console.cloud.google.com/home/dashboard?project=homeautomation-402816>
   8. <https://cloud.google.com/dns/docs/zones>
   9. <https://cloud.google.com/dns/docs/set-up-dns-records-domain-name>
   10. <https://github.com/googleapis/google-cloud-go/blob/main/domains/apiv1beta1/domains_client_example_test.go>
2. [SeeIP](https://seeip.org/)
3. Shelly
   1. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/#step-10-generate-periodic-updates-over-mqtt-using-shelly-script>
   2. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/Mqtt/#step-10-generate-periodic-updates-over-mqtt-using-shelly-script>
   3. <https://shelly-api-docs.shelly.cloud/gen2/ComponentsAndServices/HTTP/>
4. Mochi-MQTT
   1. [github.com:mochi-mqtt/server](https://github.com/mochi-mqtt/server/tree/main)
   2. [Server with TLS](https://github.com/mochi-mqtt/server/blob/main/examples/tls/main.go)
5. GoLang
  1. <https://awesome-go.com/>
  1. <https://github.com/alexflint/go-arg>
  1. <https://github.com/spf13/cobra/blob/main/site/content/user_guide.md>
6. Internet Engineering Task Force (IETF)
  1. [RFC6762: Multicast DNS](https://datatracker.ietf.org/doc/html/rfc6762)
  2. [RFC6763: DNS-Based Service Discovery](https://datatracker.ietf.org/doc/html/rfc6763)
7. HiveMQ
   1. [MQTT Topics, Wildcards, & Best Practices – MQTT Essentials: Part 5](https://www.hivemq.com/blog/mqtt-essentials-part-5-mqtt-topics-best-practices/)
8. AWS
   1. [MQTT design best practices](https://docs.aws.amazon.com/whitepapers/latest/designing-mqtt-topics-aws-iot-core/mqtt-design-best-practices.html)
9. Cedalo
   1.  [The MQTT client and its role in the MQTT connection](https://cedalo.com/blog/mqtt-connection-beginners-guide)
       1.  [Persistent Sessions](https://cedalo.com/blog/mqtt-connection-beginners-guide/#mqtt-persistent-session)
   2.  [Essential Guide to MQTT Topics and Wildcards](https://cedalo.com/blog/mqtt-topics-and-mqtt-wildcards-explained/)
