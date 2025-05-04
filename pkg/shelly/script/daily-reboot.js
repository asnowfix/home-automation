// daily-reboot.js - Shelly Plus script to randomly reboot device once per day
// Place on your Shelly Plus device (ES5 compatible)

// === CONFIGURATION ===
var REBOOT_WINDOW_START_HOUR = 2;   // Earliest hour to reboot (2 = 2:00 AM)
var REBOOT_WINDOW_END_HOUR = 5;     // Latest hour to reboot (5 = 5:59 AM)

// === INTERNAL STATE ===
var rebootScheduled = false;

function getRandomInt(min, max) {
  // Inclusive min, exclusive max
  return Math.floor(Math.random() * (max - min)) + min;
}

function scheduleRandomReboot() {
  // Get current date/time
  var now = new Date();
  var tomorrow = new Date(now.getTime() + 24*60*60*1000);
  // Pick a random hour/minute in the window
  var hour = getRandomInt(REBOOT_WINDOW_START_HOUR, REBOOT_WINDOW_END_HOUR + 1);
  var minute = getRandomInt(0, 60);
  var target = new Date(tomorrow.getFullYear(), tomorrow.getMonth(), tomorrow.getDate(), hour, minute, 0, 0);
  var delayMs = target.getTime() - now.getTime();
  print('Scheduling next reboot at', target.toISOString());
  Timer.set(delayMs, false, function() {
    print('Rebooting device now...');
    Shelly.call("Sys.Reboot", null, null);
    // After reboot, reschedule for the next day
    scheduleRandomReboot();
  });
}

// On script start, schedule today's (or next) reboot
if (!rebootScheduled) {
  scheduleRandomReboot();
  rebootScheduled = true;
}
