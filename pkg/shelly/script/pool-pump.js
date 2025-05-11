// pool-pump.js
// ------------
//
// This script controls a pool pump:
//
// 1. At startup, it ensures that only one switch is ON.
// 2. it ensures that when one switch is turned on, the other two are turned off.
// 3. When the input:0 is activated (low water), it turns off all switches, so that refill can be done.
// 4. When the input:0 is deactivated (high water), it turns on the switch that was active before the low water.
// 
// While running, ato also ensure that only one Shelly commend (eg. Switch.GetStatus / Switch.Set) is sent at a time.

let CONFIG = {
    debug: true
};

// Shelly provides print()
// if (CONFIG && CONFIG.debug) {
//  print = print;
// } else {
//   print = function() {};
// }

let switches = [0, 1, 2]; // Relay IDs for the three switches

// --- BEGIN Input:0 Event Handling ---
let input0_prev_switch_states = null;
let input0_forced_off = false;

function handleInput0Event(info) {
    print("Input:0 event", info);
    let newState = info.state;
    if (newState === true) {
        // Save current states and turn off all switches, one at a time
        input0_prev_switch_states = [];
        input0_forced_off = true;
        function processSwitch(idx) {
            if (idx >= switches.length) {
                print("All switches turned off by input:0");
                return;
            }
            var sw = switches[idx];
            Shelly.call("Switch.GetStatus", { id: sw }, function (res) {
                input0_prev_switch_states[sw] = res.output;
                Shelly.call("Switch.Set", { id: sw, on: false }, function() {
                    processSwitch(idx + 1);
                });
            });
        }
        processSwitch(0);
    } else {
        // Restore previous states
        if (input0_prev_switch_states) {
            function restoreSwitch(idx) {
                if (idx >= switches.length) {
                    print("Switch states restored after input:0 off");
                    return;
                }
                var sw = switches[idx];
                var prev = input0_prev_switch_states[sw];
                if (typeof prev === 'boolean') {
                    Shelly.call("Switch.Set", { id: sw, on: prev }, function() {
                        restoreSwitch(idx + 1);
                    });
                } else {
                    // Always use callback pattern, even if skipping
                    restoreSwitch(idx + 1);
                }
            }
            restoreSwitch(0);
        }
        input0_forced_off = false;
    }
}
// --- END Input:0 Event Handling ---

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

    print(
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

// --- Ensure only one switch is ON at startup ---
function enforceSingleSwitchOnAtStartup() {
    // Get all switch states sequentially
    var states = [];
    function checkSwitch(idx) {
        if (idx >= switches.length) {
            // Count how many are ON
            var onSwitches = [];
            for (var i = 0; i < switches.length; i++) {
                if (states[i]) onSwitches.push(i);
            }
            if (onSwitches.length > 1) {
                // Leave the first ON, turn off the rest
                function turnOffRest(offIdx) {
                    if (offIdx >= onSwitches.length) return;
                    var sw = onSwitches[offIdx];
                    if (sw !== onSwitches[0]) {
                        Shelly.call("Switch.Set", { id: sw, on: false }, function() {
                            turnOffRest(offIdx + 1);
                        });
                    } else {
                        turnOffRest(offIdx + 1);
                    }
                }
                turnOffRest(0);
            }
            return;
        }
        Shelly.call("Switch.GetStatus", { id: switches[idx] }, function(res) {
            states[idx] = res.output;
            checkSwitch(idx + 1);
        });
    }
    checkSwitch(0);
}

enforceSingleSwitchOnAtStartup();

// Subscribe to status changes for all switches and input:0 with a single handler
Shelly.addEventHandler(function (event) {
    print("event", event)
    var info = event.info;
    if (info && typeof (info.component) === "string") {
        if (info.component.indexOf("switch:") === 0 && typeof info.state === "boolean") {
            // Only allow switching if not forced off by input:0
            if (!input0_forced_off) {
                handleSwitchEvent(info);
            } else if (info.state === true) {
                // If input:0 is ON, immediately turn off any switch that tries to turn ON
                Shelly.call("Switch.Set", { id: info.id, on: false });
            }
        } else if (info.component === "input:0" && typeof info.state === "boolean") {
            handleInput0Event(info);
        }
    }
});

print("Running: pool-pump.js")