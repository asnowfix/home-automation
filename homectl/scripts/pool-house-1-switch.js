Shelly.addEventHandler(function (eventData) {
    console.log("Handling event: ", eventData);
    try {
        // main switch - pool-house-1 light
        if (eventData.id === 0 && eventData.info.event === "toggle") {
            console.log("Force pool-house-2 light to the same state");
            // MQTT.publish("shelly1minig3-54320440d02c/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://192.168.1.87/rpc/Switch.State",
                body: JSON.stringify({ "id": 0, "state": eventData.info.state })
            });
        }
    } catch (e) {
        console.log("Error handling event: ", e);
    }
});
console.log("Now handling pool-house switch events")