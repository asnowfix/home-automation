// exclusive-switch.js
// This script ensures that when one switch is turned on, the other two are turned off.

let CONFIG = {
    debug: true
};

//if (CONFIG && CONFIG.debug) {
//  print = console.log;
// } else {
//   print = function() {};
// }


let switches = [0, 1, 2]; // Relay IDs for the three switches

function handleSwitchEvent(info) {
    // var = info = {
    //     "component": "switch:1",
    //     "id": 1,
    //     "event": "toggle",
    //     "state": false,
    //     "ts": 1744662788.17999982833
    // }

    print(info);
    let activatedSwitch = info.id;
    let newState = info.state;

    console.log(
        "Switch event: id=" + activatedSwitch +
        ", state=" + newState
    );

    if (newState === true) { // Switch was turned ON
        switches.forEach(function (sw) {
            if (sw !== activatedSwitch) {
                print(
                    "Turning off switch id=" + sw +
                    " because switch id=" + activatedSwitch + " was turned ON"
                );
                // Turn off the other switches
                Shelly.call(
                    "Switch.Set",
                    { id: sw, on: false },
                    null,
                    null
                );
            }
        });
    }
}

// Subscribe to status changes for all three switches
switches.forEach(function (sw) {
    Shelly.addEventHandler(function (event) {
        // var event = {
        //     "id": 1,
        //     "now": 1744662788.17941999435,
        //     "info": {
        //         "component": "switch:1",
        //         "id": 1,
        //         "event": "toggle",
        //         "state": false,
        //         "ts": 1744662788.17999982833
        //     }
        // }
        print("event", event)
        info = event.info
        if (info && typeof (info.component) === "string" && info.component.indexOf("switch:") === 0 && info.id === sw && typeof info.state === "boolean") {
            handleSwitchEvent(info);
        }
    });
});

print("Running: exclusive-switch.js")