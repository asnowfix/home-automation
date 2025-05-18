/******************* START CHANGE HERE *******************/
let CONFIG = {
    // When set to true, debug messages will be logged to the console
    debug: true,

    // When set to true and the script ownes the scanner, the scan will be active.
    // Active scan means the scanner will ping back the Bluetooth device to receive all its data, but it will drain the battery faster
    active: false,

    // When `allowedMacAddresses` is set to null, evets from every bluetooth device are accepted.
    // allowedMacAddresses: null,

    // Initialize with empty array, will be populated by the callback
    allowedMacAddresses: [],

    topic: "groups/pool-house-lights",

    /**
     * Called when packet from filtered Shelly BLU Motion devices is received.
     * @param {Object} eventData Object, containing all parameters received from the Shelly BLU Motion device. Example: {"encryption":false,"BTHome_version":2,"pid":16,"battery":100,"illuminance":109,"motion":1,"button":1,"rssi":-53,"address":"aa:bc:12:34:56:78"}
     */
    onStatusUpdate: function (eventData) {
        // eventData = {
        //     "encryption": false,
        //     "BTHome_version": 2,
        //     "pid": 101,
        //     "battery": 98,
        //     "illuminance": 363,
        //     "motion": 1,
        //     "rssi": -75,
        //     "address": "e8:e0:7e:d0:f9:89"
        // }
        print("onStatusUpdate eventData", eventData);
        try {
            // Turn on the light if the motion is detected & illuminance is below 100.
            if (eventData.motion === 1 && eventData.illuminance < 100) {
                msg = JSON.stringify({ "op": "on", "keep": false })
                print("MQTT publishing: ", "topic", CONFIG.topic, "msg", msg)
                MQTT.publish(CONFIG.topic, msg, 2 /*exactly-once*/, false);
            }
        } catch (error) {
            console.error("onStatusUpdate error", error);
        }
    },
    turnOnIfOff: function (result, error_code, error_message, user_data) {
        //print("turnOnIfOff result:", result, "error_code:", error_code, "error_message:", error_message, "user_data:", user_data);
        if (result.output === false) {
            print("switch off: turn it on, with auto-off after", CONFIG.autoOffTimeout, "seconds")
            Shelly.call("Switch.Set", { id: 0, on: true });
            function timerCode() {
                print("auto-off: turning off the switch")
                Shelly.call("Switch.Set", { id: 0, on: false });
            };
            Timer.set(
            /* number of miliseconds */ 1000 * CONFIG.autoOffTimeout,
            /* repeat? */ false,
            /* callback */ timerCode
            );
        } else {
            print("switch already on ")
        }
    }
};

/******************* STOP CHANGE HERE *******************/

let ALLTERCO_MFD_ID_STR = "0ba9";
let BTHOME_SVC_ID_STR = "fcd2";

let uint8 = 0;
let int8 = 1;
let uint16 = 2;
let int16 = 3;
let uint24 = 4;
let int24 = 5;

//Logs the provided message with an optional prefix to the console.
function logger(message, prefix) {
    //exit if the debug isn't enabled
    if (!CONFIG.debug) {
        return;
    }

    let finalText = "";

    //if the message is list loop over it
    if (Array.isArray(message)) {
        for (let i = 0; i < message.length; i++) {
            finalText = finalText + " " + JSON.stringify(message[i]);
        }
    } else {
        finalText = JSON.stringify(message);
    }

    //the prefix must be string
    if (typeof prefix !== "string") {
        prefix = "";
    } else {
        prefix = prefix + ":";
    }

    //log the result
    print(prefix, finalText);
}

// The BTH object defines the structure of the BTHome data
let BTH = {};
BTH[0x00] = { n: "pid", t: uint8 };
BTH[0x01] = { n: "battery", t: uint8, u: "%" };
BTH[0x02] = { n: "temperature", t: int16, f: 0.01, u: "tC" };
BTH[0x03] = { n: "humidity", t: uint16, f: 0.01, u: "%" };
BTH[0x05] = { n: "illuminance", t: uint24, f: 0.01 };
BTH[0x21] = { n: "motion", t: uint8 };
BTH[0x2d] = { n: "window", t: uint8 };
BTH[0x3a] = { n: "button", t: uint8 };
BTH[0x3f] = { n: "rotation", t: int16, f: 0.1 };

function getByteSize(type) {
    if (type === uint8 || type === int8) return 1;
    if (type === uint16 || type === int16) return 2;
    if (type === uint24 || type === int24) return 3;
    //impossible as advertisements are much smaller;
    return 255;
}

// functions for decoding and unpacking the service data from Shelly BLU devices
let BTHomeDecoder = {
    utoi: function (num, bitsz) {
        let mask = 1 << (bitsz - 1);
        return num & mask ? num - (1 << bitsz) : num;
    },
    getUInt8: function (buffer) {
        return buffer.at(0);
    },
    getInt8: function (buffer) {
        return this.utoi(this.getUInt8(buffer), 8);
    },
    getUInt16LE: function (buffer) {
        return 0xffff & ((buffer.at(1) << 8) | buffer.at(0));
    },
    getInt16LE: function (buffer) {
        return this.utoi(this.getUInt16LE(buffer), 16);
    },
    getUInt24LE: function (buffer) {
        return (
            0x00ffffff & ((buffer.at(2) << 16) | (buffer.at(1) << 8) | buffer.at(0))
        );
    },
    getInt24LE: function (buffer) {
        return this.utoi(this.getUInt24LE(buffer), 24);
    },
    getBufValue: function (type, buffer) {
        if (buffer.length < getByteSize(type)) return null;
        let res = null;
        if (type === uint8) res = this.getUInt8(buffer);
        if (type === int8) res = this.getInt8(buffer);
        if (type === uint16) res = this.getUInt16LE(buffer);
        if (type === int16) res = this.getInt16LE(buffer);
        if (type === uint24) res = this.getUInt24LE(buffer);
        if (type === int24) res = this.getInt24LE(buffer);
        return res;
    },

    // Unpacks the service data buffer from a Shelly BLU device
    unpack: function (buffer) {
        //beacons might not provide BTH service data
        if (typeof buffer !== "string" || buffer.length === 0) return null;
        let result = {};
        let _dib = buffer.at(0);
        result["encryption"] = _dib & 0x1 ? true : false;
        result["BTHome_version"] = _dib >> 5;
        if (result["BTHome_version"] !== 2) return null;
        //can not handle encrypted data
        if (result["encryption"]) return result;
        buffer = buffer.slice(1);

        let _bth;
        let _value;
        while (buffer.length > 0) {
            _bth = BTH[buffer.at(0)];
            if (typeof _bth === "undefined") {
                logger("unknown type", "BTH");
                break;
            }
            buffer = buffer.slice(1);
            _value = this.getBufValue(_bth.t, buffer);
            if (_value === null) break;
            if (typeof _bth.f !== "undefined") _value = _value * _bth.f;
            result[_bth.n] = _value;
            buffer = buffer.slice(getByteSize(_bth.t));
        }
        return result;
    },
};

function onReceivedPacket(data) {
    if (CONFIG._processedMacAddresses !== null) {
        if (CONFIG._processedMacAddresses.indexOf(data.address) < 0) {
            logger(["Received event from", data.address, "outside of the allowed addresses"], "Info");
            return;
        }
    }

    if (typeof CONFIG.onStatusUpdate === "function") {
        CONFIG.onStatusUpdate(data);
        logger("New status update", "Info");
    }
}

//saving the id of the last packet, this is used to filter the duplicated packets
let lastPacketId = 0x100;

// Callback for the BLE scanner object
function BLEScanCallback(event, result) {
    //exit if not a result of a scan
    if (event !== BLE.Scanner.SCAN_RESULT) {
        return;
    }

    //exit if service_data member is missing
    if (
        typeof result.service_data === "undefined" ||
        typeof result.service_data[BTHOME_SVC_ID_STR] === "undefined"
    ) {
        return;
    }

    let unpackedData = BTHomeDecoder.unpack(
        result.service_data[BTHOME_SVC_ID_STR]
    );

    //exit if unpacked data is null or the device is encrypted
    if (
        unpackedData === null ||
        typeof unpackedData === "undefined" ||
        unpackedData["encryption"]
    ) {
        logger("Encrypted devices are not supported", "Error");
        return;
    }

    //exit if the event is duplicated
    if (lastPacketId === unpackedData.pid) {
        return;
    }

    lastPacketId = unpackedData.pid;

    unpackedData.rssi = result.rssi;
    unpackedData.address = result.addr;

    onReceivedPacket(unpackedData);
}

// Initializes the script and performs the necessary checks and configurations
function init() {
    print("Initializing ble-shelly-motion.js")

    //exit if can't find the config
    if (typeof CONFIG === "undefined") {
        print("Error: Undefined CONFIG");
        return;
    }

    //get the config of ble component
    let BLEConfig = Shelly.getComponentConfig("ble");

    //exit if the BLE isn't enabled
    if (!BLEConfig.enable) {
        print(
            "Error: The Bluetooth is not enabled, please enable it from settings"
        );
        return;
    }
    print("BLE config:", JSON.stringify(BLEConfig));

    //check if the scanner is already running
    if (BLE.Scanner.isRunning()) {
        print("Info: The BLE gateway is running, the BLE scan configuration is managed by the device");
    } else {
        print("Starting the scanner");
        //start the scanner
        let bleScanner = BLE.Scanner.Start({
            duration_ms: BLE.Scanner.INFINITE_SCAN,
            active: CONFIG.active
        });

        if (!bleScanner) {
            print("Error: Can not start new scanner");
        }
    }

    CONFIG._processedMacAddresses = null;

    print("Loading allowed MAC addresses from KVS");
    Shelly.call("KVS.Get", { key: "following/ble-shelly-motion" }, function (result, error_code, error_message) {
        if (error_code === 0 && result) {
            try {
                print("Result:", result);
                CONFIG.allowedMacAddresses = JSON.parse(result.value || "[]");
                print("Loaded allowed MAC addresses:", JSON.stringify(CONFIG.allowedMacAddresses));

                // Process configured mac addresses all to lower case and remove duplicates.
                CONFIG._processedMacAddresses =
                    CONFIG
                        .allowedMacAddresses
                        .map(function (mac) { return mac.toLowerCase(); })
                        .filter(function (value, index, array) { return array.indexOf(value) === index; })

                print("Subscribing to BLE scanner");
                BLE.Scanner.Subscribe(BLEScanCallback);
            } catch (e) {
                print("Error: Can not parse allowed MAC addresses", e);
            }
        } else {
            print("Error: Can not load allowed MAC addresses from KVS", error_code, error_message);
        }
    });

}

init();
