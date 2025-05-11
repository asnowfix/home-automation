// Script to be uploaded on any switch that is meant to control a group of relays (here: 'group/pool-house-lights)
let CONFIG = {
    topic: "groups/pool-house-lights",
}
Shelly.addEventHandler(function (eventData) {
    print("Handling event: ", eventData);
    try {
        var msg = null
        if (eventData.id === 0 && eventData.info.event === "single_push") {
            print("Button single push: toggle")
            msg = JSON.stringify({"op":"toggle"})
        } else if (eventData.id === 0 && eventData.info.event === "double_push") {
            print("Button double push: turn on & keep-on")
            msg = JSON.stringify({"op":"on", "keep": true})
        }
        // else if (eventData.id === 0 && eventData.info.event === "long_push") {
        //     print("Button long push: turn on & keep-on")
        //     msg = JSON.stringify({"op":"on", "keep": false})
        // }
        if (msg !== null) {
            print("MQTT publishing: ", "topic", CONFIG.topic, "msg", msg)
            MQTT.publish(CONFIG.topic, msg, 2 /*exactly-once*/, false);
        }
    } catch (e) {
        print("Error handling event: ", e);
    }
});
print("Now sending switch events to topic ", CONFIG.topic)