Shelly.addEventHandler(function (eventData) {
    console.log("Handling event: ", eventData);
    try {
        // main switch - pool-house-1 light
        if (eventData.id === 0 && eventData.info.event === "btn_up") {
            console.log("Toggle pool-house-1 light");
            MQTT.publish("shellyplus1-b8d61a85a8e0/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shellyplus1-b8d61a85a8e0.local/rpc/Switch.Toogle",
            //     body: JSON.stringify({ "id": 0 })
            // });

            console.log("Toggle pool-house-2 light");
            MQTT.publish("shelly1minig3-54320440d02c/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shelly1minig3-54320440d02c.local/rpc/Switch.Toogle",
            //     body: JSON.stringify({ "id": 0 })
            // });
        }
    } catch (e) {
        console.log("Error handling event: ", e);
    }
});
console.log("Now handling pool-house switch events")