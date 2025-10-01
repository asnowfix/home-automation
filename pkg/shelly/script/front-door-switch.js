Shelly.addEventHandler(function (event) {
    print("Handling event: ", event);
    try {
        if (event.id === 1 && event.info.state === true) {
            print("Toogling right door light");
            MQTT.publish("shellyplus1-b8d61a85ed58/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shellyplus1-b8d61a85ed58.local/rpc/Switch.Toggle",
            //     body: JSON.stringify({ "id": 0 })
            // });
            print("Toogling front door light");
            MQTT.publish("shelly1minig3-543204522cb4/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shelly1minig3-543204522cb4.local/rpc/Switch.Toggle",
            //     body: JSON.stringify({ "id": 0 })
            // });
            print("Toogling old-front door light");
            MQTT.publish("shelly1minig3-54320464a1d0/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shelly1minig3-54320464a1d0.local/rpc/Switch.Toggle",
            //     body: JSON.stringify({ "id": 0 })
            // });
        }
        // top-right switch - lustre
        if (event.id === 0 && event.info.state === true) {
            print("Toggle lustre light");
            MQTT.publish("shelly1minig3-84fce63bf464/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.POST", {
            //     url: "http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle",
            //     body: JSON.stringify({ "id": 0 })
            // });
        }
        // bottom-right switch - lumiere-parking
        if (event.id === 3 && event.info.state === true) {
            print("Toggle lumiere-parking light");
            MQTT.publish("shellypro2-2cbcbb9fb834/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":1}}), 0, false);
            // Shelly.call("HTTP.GET", {
            //   url: 'http://shellypro2-2cbcbb9fb834.local/rpc/HTTP.GET?url="http://192.168.33.18/rpc/Switch.Toggle?id=1"'
            // });
        }
        // bottom-left switch - external-stairs
        if (event.id === 2 && event.info.state === true) {
            print("Toggle external-stairs light");
            // Call "escalier-exterieur.local" via "old-front-door-light.local"
            // $ curl -X GET "http://shelly1minig3-543204641d24.local/rpc/HTTP.GET?url=\"http://192.168.33.18/rpc/Switch.Toggle?id=0\""
            // {"code":200, "message":"OK", "headers":{"Connection": "close","Content-Length": "15","Content-Type": "application/json","Server": "ShellyHTTP/1.0.0"}, "body":"{\"was_on\":true}"}
            MQTT.publish("shelly1minig3-543204641d24/rpc", JSON.stringify({"method":"Switch.Toggle", "params":{"id":0}}), 0, false);
            // Shelly.call("HTTP.GET", {
            //   url: 'http://shelly1minig3-543204641d24.local/rpc/HTTP.GET?url="http://192.168.33.18/rpc/Switch.Toggle?id=0"'
            // });
        }
    } catch (e) {
        print("Error handling event: ", e);
    }
});
print("Now handling front-door switch events")