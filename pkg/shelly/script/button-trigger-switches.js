// button-trigger-switches.js
let CONFIG = {
    switches: [ "shelly1minig3-54320464074c", "shelly1minig3-54320440d02c" ],
    channel: "mqtt",
}
Shelly.addEventHandler(function (eventData) {
    print("Handling event: ", eventData);
    try {
        if (eventData.id === 0 && eventData.info.event === "btn_up") {
            for (let i = 0; i < CONFIG.switches.length; i++) {
                if (CONFIG.channel === "mqtt") {
                    topic = CONFIG.switches[i] + "/rpc"
                    msg = JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}})
                    print("MQTT publishing: ", "topic", topic, "msg", msg)
                    MQTT.publish(topic, msg, 2 /*exactly-once*/, false);
                } else if (CONFIG.channel === "http") {
                    url = "http://" + CONFIG.switches[i] + "/rpc/Switch.Toogle"
                    print("HTTP publishing: ", "url", url)
                    Shelly.call("HTTP.POST", {
                        url: url,
                        body: JSON.stringify({ "id": 0 })
                    });
                }
            }
        }
    } catch (e) {
        print("Error handling event: ", e);
    }
});
print("Will emit events to switches: ", JSON.stringify(CONFIG.switches))