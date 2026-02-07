package global

import (
	"context"
)

type ContextKey uint

const (
	CancelKey ContextKey = iota
	CpuProfileKey
	VersionKey
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
