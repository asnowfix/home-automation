package ui

import "sync/atomic"

// starting is true from daemon start until the first full device refresh
// pass has run (#763). While it is, a switch whose state is not known yet
// renders "Starting…" instead of a bare unknown: the daemon has not yet had
// its turn to look at that device, which is different from a device that was
// looked at and did not answer.
//
// It is one process-wide flag, not per-device state: the refresh round-robin
// (DeviceManager.runDeviceRefreshJob) visits every Gen2 device once per pass,
// so "pass done" means "every device has been tried". The default is false, so
// the CLI and tests that never call SetStarting render plain unknown.
var starting atomic.Bool

// SetStarting turns the post-start "Starting…" phase on or off. The device
// manager sets it before its refresh job runs and clears it, on every exit
// path of that job, once the first pass is over.
func SetStarting(v bool) { starting.Store(v) }

// IsStarting reports whether the post-start phase is still on.
func IsStarting() bool { return starting.Load() }
