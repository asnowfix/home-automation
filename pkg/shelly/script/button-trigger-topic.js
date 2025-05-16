// Script to be uploaded on any switch that is meant to control a group of relays (here: 'group/pool-house-lights)
let CONFIG = {
    topic: "groups/pool-house-lights",
    longPressThreshold: 1500,
}

let pressStart = null;
let msg = null;

Shelly.addEventHandler(function (event) {
    if (event.component === "input:0") {
        if (event.event === "btn_up") {
            print("Button pressed")
            pressStart = Timer.now();
        } else if (event.event === "btn_down") {
            if (pressStart !== null) {
                let duration = (Timer.now() - pressStart) * 1000; // convert to ms
                print("Button released, duration: ", duration)
                if (duration >= CONFIG.longPressThreshold) {
                    print("Custom long push detected:", duration, "ms");
                    // Perform long push action
                    msg = JSON.stringify({ "op": "on", "keep": false })
                } else {
                    print("Short push detected:", duration, "ms");
                    // Perform short push or let system handle double_push etc.
                    msg = JSON.stringify({ "op": "toggle" })
                }
                pressStart = null;
                if (msg !== null) {
                    print("MQTT publishing: ", "topic", CONFIG.topic, "msg", msg)
                    MQTT.publish(CONFIG.topic, msg, 2 /*exactly-once*/, false);
                }
            }
        }
    }
});

print("Now sending switch events to topic ", CONFIG.topic)