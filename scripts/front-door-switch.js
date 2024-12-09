Shelly.addEventHandler(function (event) {
    console.log("Handling event: ", event);
    try {
        // top-left switch - front door light
        if (event.id === 1 && event.info.state === true) {
            console.log("Toogling front door light");
            // MQTT.publish("shelly1minig3-543204522cb4/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://192.168.1.28/rpc/Switch.Toggle",
                body: JSON.stringify({ "id": 0 })
            });
        }
        if (event.id === 0 && event.info.state === true) {
            console.log("Toggle lustre light");
            // MQTT.publish("shelly1minig3-84fce63bf464/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://192.168.1.40/rpc/Switch.Toggle",
                body: JSON.stringify({ "id": 0 })
            });
        }
    } catch (e) {
        console.log("Error handling event: ", e);
    }
});
console.log("Now handling front-door switch events")