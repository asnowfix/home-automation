let CONFIG = {
    // TODO:configurable via KVS
    // light-outside-steps / escalier-exterieur
    device: "shelly1minig3-54320464f17c",
};
MQTT.subscribe(
    CONFIG.device + "/events/rpc",
    function (topic, message, ud) {
        try {
            console.log("Handling event topic:", topic, "message", message, "userData", ud);
            // message = {
            //     "src":"shelly1minig3-54320464f17c",
            //     "dst":"shelly1minig3-54320464f17c/events",
            //     "method":"NotifyStatus",
            //     "params":{
            //         "ts":1745708765.60,
            //         "switch:0":{
            //             "id":0,
            //             "output":false,
            //             "source":"MQTT"
            //         }
            //     }
            // }
            msg = JSON.parse(message)
            if (msg.method === "NotifyStatus" && msg.params["switch:0"] !== undefined && msg.params["switch:0"].output !== undefined) {
                console.log("Calling Switch.Set", msg.params["switch:0"].output, "for", CONFIG.device)
                Shelly.call("Switch.Set", { id: 1, on: msg.params["switch:0"].output });
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