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
    script: "[pool-pump]",
    debug: true
};

let log = function() {
    if (CONFIG && CONFIG.debug) {
        let args = [CONFIG.script];
        for (let i = 0; i < arguments.length; i++) {
            if (typeof arguments[i] === "object") {
                args.push(JSON.stringify(arguments[i]));
            } else {
                args.push(arguments[i]);
            }
        }
        print.apply(null, args);
    }
};

let switches = [0, 1, 2]; // Relay IDs for the three switches

// --- BEGIN Input:0 Event Handling ---
let input0_prev_switch_states = null;
let input0_forced_off = false;

// When input:0 is activated (low water), it turns off all switches, so that refill can be done.
// When input:0 is deactivated (high water), it turns on the switch that was active before the low water.
function handleInput0Event(info) {
    log("Input:0 info", info);
    let newState = info.state;
    if (newState === true) {
        // Save current states and turn off all switches, one at a time
        input0_prev_switch_states = [];
        input0_forced_off = true;
        function processSwitch(idx) {
            if (idx >= switches.length) {
                log("All switches turned off by input:0");
                return;
            }

            let component = "switch:" + switches[idx];
            let status = Shelly.getComponentStatus(component);
            log("Switch status", component, status);

            input0_prev_switch_states[switches[idx]] = status.output;
            Shelly.call("Switch.Set", { id: switches[idx], on: false }, function() {
                processSwitch(idx + 1);
            });
        }
        processSwitch(0);
    } else {
        // Restore previous states
        if (input0_prev_switch_states) {
            function restoreSwitch(idx) {
                if (idx >= switches.length) {
                    log("Switch states restored after input:0 off");
                    return;
                }
                let sw = switches[idx];
                let prev = input0_prev_switch_states[sw];
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

    log("handleSwitchEvent:", info);
    let activatedSwitch = info.id;
    let newState = info.state;

    log(
        "Switch event: id=" + activatedSwitch +
        ", state=" + newState
    );

    if (newState === true) { // Switch was turned ON
        switches.forEach(function (sw) {
            if (sw !== activatedSwitch) {
                log(
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
    let states = [];
    function checkSwitch(idx) {
        if (idx >= switches.length) {
            // Count how many are ON
            let onSwitches = [];
            for (let i = 0; i < switches.length; i++) {
                if (states[i]) onSwitches.push(i);
            }
            if (onSwitches.length > 1) {
                // Leave the first ON, turn off the rest
                function turnOffRest(offIdx) {
                    if (offIdx >= onSwitches.length) return;
                    let sw = onSwitches[offIdx];
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
        let component = "switch:" + switches[idx];
        let status = Shelly.getComponentStatus(component);
        log("Switch status", component, status);
        states[idx] = status.output;
        checkSwitch(idx + 1);
    }
    checkSwitch(0);
}

enforceSingleSwitchOnAtStartup();

// Subscribe to status changes for all switches and input:0 with a single handler
Shelly.addEventHandler(function (event) {
    log("event", event)
    let info = event.info;
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

log("Running")