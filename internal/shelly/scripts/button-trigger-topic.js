// Script to be uploaded on any switch that is meant to control a group of relays (here: 'group/pool-house-lights)

print("Loading button-trigger-topic.js")

let CONFIG = {
    topic: "some-listen-to-topic",
    longPushThreshold: 1500,
    detectLongPush: false,
    down_event: "btn_down",
    up_event: "btn_up",
}

let pressStart = null;

// 'long_push' works if & only if inverted-logic is set to false
//
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_down\",\"ts\":1747394703.87}" ts=29942.841 v=0
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"long_push\",\"ts\":1747394704.87}" ts=29943.841 v=0
// msg="shelly_notification:210 Event from input:0: {\"component\":\"input:0\",\"id\":0,\"event\":\"btn_up\",\"ts\":1747394707.80}" ts=29946.772 v=0

let config = Shelly.getComponentConfig("input:0");
// config = {"result":"{\"id\":0,\"name\":null,\"type\":\"button\",\"enable\":true,\"invert\":false,\"factory_reset\":true}"}
print("Shelly.getComponentConfig():", JSON.stringify(config))
if (config.invert) {
    print("Inverted logic detected")
    // Use 'btn_up' & 'btn_down' events if invert is true
    CONFIG.detectLongPush = true;
    CONFIG.down_event = "btn_up";
    CONFIG.up_event = "btn_down";
} else {
    print("Normal logic detected")
    // Use 'long_push' event if invert is false
    CONFIG.detectLongPush = false;
    CONFIG.down_event = "btn_down";
    CONFIG.up_event = "btn_up";
}

Shelly.addEventHandler(function (data) {
    try {
        let msg = null;
        if (data.component === "input:0") {
            // print("Got data:", JSON.stringify(data))
            if (!CONFIG.detectLongPush) {
                pressStart = null;
                if (data.info.event === "long_push") {
                    print("Long push detected")
                    // Perform long push action
                    msg = JSON.stringify({ "op": "on", "keep": true })
                } else if (data.info.event === "single_push") {
                    print("Single push detected")
                    msg = JSON.stringify({ "op": "toggle" })
                }
            } else {
                if (data.info.event === CONFIG.down_event) {
                    print("Button pressed")
                    pressStart = Timer.now();
                } else if (data.info.event === CONFIG.up_event) {
                    if (pressStart !== null) {
                        let duration = (Timer.now() - pressStart) * 1000; // convert to ms
                        print("Button released, duration: ", duration)
                        if (duration >= CONFIG.longPushThreshold) {
                            print("Custom long push detected:", duration, "ms");
                            // Perform long push action
                            msg = JSON.stringify({ "op": "on", "keep": true })
                        } else {
                            print("Short push detected:", duration, "ms");
                            // Perform short push or let system handle double_push etc.
                            msg = JSON.stringify({ "op": "toggle" })
                        }
                        pressStart = null;
                    }
                }
            }
            if (msg !== null) {
                print("MQTT publishing: ", "topic", CONFIG.topic, "msg", msg)
                MQTT.publish(CONFIG.topic, msg, 2 /*exactly-once*/, false);
            } else {
                // print("Discarding data:", JSON.stringify(data))
            }
        }
    } catch (e) {
        print("*** error:", e, "data:", JSON.stringify(data));
    }
});

print("Now sending switch events to topic ", CONFIG.topic)