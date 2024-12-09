Shelly.addEventHandler(function (event) {
    console.log("Handling event: ", event);
    try {
        // top-left switch - front door light
        if (event.id === 1 && event.info.state === true) {
            console.log("Toogling front door light");
            // MQTT.publish("shelly1minig3-543204522cb4/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://shelly1minig3-543204522cb4.local/rpc/Switch.Toggle",
                body: JSON.stringify({ "id": 0 })
            });
            console.log("Toogling old-front door light");
            // MQTT.publish("shelly1minig3-54320464a1d0/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://shelly1minig3-54320464a1d0.local/rpc/Switch.Toggle",
                body: JSON.stringify({ "id": 0 })
            });
        }
        // top-right switch - lustre
        if (event.id === 0 && event.info.state === true) {
            console.log("Toggle lustre light");
            // MQTT.publish("shelly1minig3-84fce63bf464/rpc", JSON.stringify(true), 0, false);
            Shelly.call("HTTP.POST", {
                url: "http://shelly1minig3-84fce63bf464.local/rpc/Switch.Toggle",
                body: JSON.stringify({ "id": 0 })
            });
        }
        // bottom-right switch - external-stairs
        if (event.id === 3 && event.info.state === true) {
            console.log("Toggle external-stairs light");
            // Call "escalier-exterieur.local" via "old-front-door-light.local"
            // $ curl -X GET "http://shelly1minig3-54320464a1d0.local/rpc/HTTP.GET?url=\"http://192.168.33.18/rpc/Switch.Toggle?id=0\""
            // {"code":200, "message":"OK", "headers":{"Connection": "close","Content-Length": "15","Content-Type": "application/json","Server": "ShellyHTTP/1.0.0"}, "body":"{\"was_on\":true}"}
            //MQTT.publish("shelly1minig3-54320464f17c/rpc/Switch.Toggle", JSON.stringify({ "id": 0 }), 0, false);
            Shelly.call("HTTP.GET", {
              url: 'http://shelly1minig3-54320464a1d0.local/rpc/HTTP.GET?url="http://192.168.33.18/rpc/Switch.Toggle?id=0"'
            });
        }
    } catch (e) {
        console.log("Error handling event: ", e);
    }
});
console.log("Now handling front-door switch events")