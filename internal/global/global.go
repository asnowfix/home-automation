package global

import (
	"context"
)

type ContextKey uint

const (
	// VersionKey and ProcessContextKey carry request-scoped data that's
	// legitimately read far from where it's set. Control-flow dependencies
	// (a CancelFunc, an *os.File) are NOT stored here — see myhome/main.go,
	// myhome/ctl/ctl.go and myhome/daemon/daemon.go, which each own their
	// cancellation/profile lifecycle explicitly instead.
	VersionKey ContextKey = iota
	ProcessContextKey
)

// PanicOnBugs controls whether programming errors (e.g. missing context values)
// should panic instead of returning errors. Enabled by --panic-on-bugs or --debug.
var PanicOnBugs bool

func Version(ctx context.Context) string {
	return ctx.Value(VersionKey).(string)
}

// ProcessContext returns the process-wide context for lazy-started background services.
// This context is cancelled only when the entire process terminates, not on individual operation completion.
func ProcessContext(ctx context.Context) context.Context {
	if processCtx := ctx.Value(ProcessContextKey); processCtx != nil {
		return processCtx.(context.Context)
	}
	// Fallback to the current context if no process context is stored
	return ctx
}
