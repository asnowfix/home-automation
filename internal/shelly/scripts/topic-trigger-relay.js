// topic-trigger-relay.js
// Wait for MQTT messages on the topic 'groups/pool-house-lights' and acts on the relay accordingly.

let CONFIG = {
    autoOffTimeout: 300,
    topic: "groups/pool-house-lights",
}

let VARS = {
    timer: null,
}

MQTT.subscribe(CONFIG.topic, function (topic, message) {
    print("Received message on topic: ", topic, "message: ", message);
    try {
        let data = JSON.parse(message);
        if (data.op === "toggle") {
            print("Toggle")
            Shelly.call("Switch.Toggle", { id: 0 });
            Timer.clear(VARS.timer);
        }
        if (data.op === "on") {
            Shelly.call("Switch.Set", { id: 0, on: true });
            Timer.clear(VARS.timer);
            if (data.keep === true) {
                print("Turn & keep-on")
            } else {
                print("Turn on & auto-off")
                VARS.timer = Timer.set(
                    /* number of miliseconds */ 1000 * CONFIG.autoOffTimeout,
                    /* repeat? */ false,
                    /* callback */ function () {
                        Shelly.call("Switch.Set", { id: 0, on: false });
                    }
                );
            }
        }
        if (data.op === "off") {
            Shelly.call("Switch.Set", { id: 0, on: false });
            Timer.clear(VARS.timer);
        }
    } catch (e) {
        print("Error parsing message: ", e);
    }
});

print("Now handling MQTT messages on topic ", CONFIG.topic)