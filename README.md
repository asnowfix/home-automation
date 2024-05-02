# Home Automation

## Abstract

Collection of tools to help automating my own House, mostly using (very cool) Shelly devices, from Alterco Robotics.

## Table of Contents <!-- omit in toc -->

- [Home Automation](#home-automation)
  - [Abstract](#abstract)
  - [Usage](#usage)
    - [Is daemon running?](#is-daemon-running)
    - [Manual start](#manual-start)
  - [Shelly Notes](#shelly-notes)
    - [Shelly 1 H\&T](#shelly-1-ht)
    - [Shelly MQTT Notes](#shelly-mqtt-notes)
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

## Usage

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

### Shelly MQTT Notes

- [Hive MQTT CLI Installation](https://hivemq.github.io/mqtt-cli/docs/installation/)

Subscribe to Shelly H&T Gen1:

```shell
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
