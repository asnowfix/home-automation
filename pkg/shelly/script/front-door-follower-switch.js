let CONFIG = {
    // front-door light
    device: "shelly1minig3-543204522cb4",
};
MQTT.subscribe(
    CONFIG.device + "/events/rpc",
    function (topic, message, ud) {
        try {
            console.log("Handling event topic:", topic, "message", message, "userData", ud);
            // message = {
            //     "src": "shelly1minig3-543204522cb4",
            //     "dst": "shelly1minig3-54323:58:54204522cb4/events",
            //     "method": "NotifyStatus",
            //     "params": {
            //         "ts": 1734476334.51,
            //         "switch:0": {
            //             "id": 0,
            //             "output": true,
            //             "source": "HTTP_in"
            //         }
            //     }
            // }
            msg = JSON.parse(message)
            if (msg.method === "NotifyStatus") {
                console.log("Calling Switch.Set", msg.params["switch:0"].output)
                Shelly.call("Switch.Set", { id: 0, on: msg.params["switch:0"].output });
            } else {
              console.log("ignoring msg:", msg)
            }
        } catch (e) {
            console.log("Error handling event: ", e);
        }
    },
    "none"
);
console.log("Now handling MQTT RPC events from", CONFIG.device);