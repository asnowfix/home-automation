Shelly.addEventHandler(function (event) {
    console.log("Handling event: ", event);
    // if (event.name === "input" && event.id === 100 && event.info.state === true) {
    // MQTT.publish("shellyplus1-a8032abd2900/doormoving", JSON.stringify(true), 0, false);
    // }
});
console.log("Now logging events")