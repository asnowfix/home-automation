#!/usr/bin/env python3
"""Read one retained MQTT message and print "<available_w>|<stale>".

A minimal, deliberately dependency-free MQTT 3.1.1 subscriber: this repo vendors
no Python MQTT library, and `mosquitto_sub` is not present on every machine that
needs to run tools/monitor-solar.sh. Roughly 40 lines beats adding a dependency
for one retained read.

Environment:
  BROKER_HOST  default 192.168.1.2
  BROKER_PORT  default 1883
  SOLAR_TOPIC  default myhome/energy/solar/available

Output is a single line, always:
  "<watts>|<stale>"   e.g. "720|False"
  "?|?"               nothing retained arrived within the timeout
  "ERR|<reason>"      connection or parse failure

It never raises. The caller logs this field verbatim into a CSV column and must
not die because the broker hiccuped - transient MQTT failures against this
broker are real and observed. See docs/monitor-solar.md.
"""
import json
import os
import socket
import struct
import time


def main():
    host = os.environ.get("BROKER_HOST", "192.168.1.2")
    port = int(os.environ.get("BROKER_PORT", "1883"))
    topic = os.environ.get("SOLAR_TOPIC", "myhome/energy/solar/available").encode()
    try:
        sock = socket.create_connection((host, port), timeout=6)

        def read_exactly(n):
            buf = b""
            while len(buf) < n:
                chunk = sock.recv(n - len(buf))
                if not chunk:
                    raise EOFError("connection closed")
                buf += chunk
            return buf

        def remaining_length():
            mult, value = 1, 0
            while True:
                byte = read_exactly(1)[0]
                value += (byte & 127) * mult
                if not byte & 128:
                    return value
                mult *= 128

        client_id = ("solarprobe-%d" % (time.time() % 100000)).encode()
        payload = b"\x00\x04MQTT\x04\x02\x00\x3c" + struct.pack("!H", len(client_id)) + client_id
        sock.sendall(b"\x10" + bytes([len(payload)]) + payload)
        read_exactly(1)
        remaining_length()
        read_exactly(2)  # CONNACK

        payload = struct.pack("!H", 1) + struct.pack("!H", len(topic)) + topic + b"\x00"
        sock.sendall(b"\x82" + bytes([len(payload)]) + payload)

        sock.settimeout(6)
        deadline = time.time() + 6
        while time.time() < deadline:
            header = read_exactly(1)[0]
            length = remaining_length()
            body = read_exactly(length) if length else b""
            if header >> 4 == 3:  # PUBLISH
                topic_len = struct.unpack("!H", body[:2])[0]
                data = json.loads(body[2 + topic_len:].decode())
                sources = data.get("sources") or [{}]
                print("%s|%s" % (data.get("available_w", "?"), sources[0].get("stale", "?")))
                return
        print("?|?")
    except Exception as exc:  # noqa: BLE001 - see module docstring
        print("ERR|%s" % str(exc)[:30])


main()
