package global

import "context"

type ContextKey uint

const (
	CancelKey ContextKey = iota
	LogKey
	CpuProfileKey
	VersionKey
)

func Version(ctx context.Context) string {
	return ctx.Value(VersionKey).(string)
}
