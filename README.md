# Home Automation

## Abstract

Collection of tools to help automating my own House

## Table of Contents <!-- omit in toc -->

1. [Abstract](#abstract)
2. [GCP Notes](#gcp-notes)
3. [Mochi-MQTT Notes](#mochi-mqtt-notes)
4. [References](#references)

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

## Shelly Notes

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
