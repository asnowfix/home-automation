Shelly.addEventHandler(function (eventData) {
    console.log("Handling event: ", eventData);
    try {
        if (eventData.id === 0 && eventData.info.event === "btn_up") {
            console.log("Toggle light-pool-house-1");
            MQTT.publish("shelly1minig3-54320464074c/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shelly1minig3-54320464074c.local/rpc/Switch.Toogle",
            //     body: JSON.stringify({ "id": 0 })
            // });

            console.log("Toggle light-pool-house-2");
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