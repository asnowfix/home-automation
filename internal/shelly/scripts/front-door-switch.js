Shelly.addEventHandler(function (event) {
    print("Handling event: ", event);
    try {
        // top-left switch
        if (event.id === 1 && event.info.state === true) {
            print("Toggle lampe-buffet-entree");
            MQTT.publish("shellyplugsg3-e4b323382ea4/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0 /*at most once*/, false /*retain*/);
        }
        // top-right switch
        if (event.id === 0 && event.info.state === true) {
            print("Toggle lustre light");
            MQTT.publish("shelly1minig3-84fce63bf464/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0 /*at most once*/, false /*retain*/);
        }
        // bottom-right switch
        if (event.id === 3 && event.info.state === true) {
            print("Toggle lumiere-parking light");
            MQTT.publish("shellypro2-2cbcbb9fb834/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":1}}), 0 /*at most once*/, false /*retain*/);
        }
        // bottom-left switch
        if (event.id === 2 && event.info.state === true) {
            print("Toogling front door light");
            MQTT.publish("shelly1minig3-543204522cb4/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0 /*at most once*/, false /*retain*/);
        }
    } catch (e) {
        print("Error handling event: ", e);
    }
});
print("Now handling front-door switch events")