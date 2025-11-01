package global

import (
	"context"

	"github.com/go-logr/logr"
)

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

// ContextWithoutTimeout creates a new background context but preserves logger and version from the original context
// This is useful for long-running operations that shouldn't be bound by the command timeout
func ContextWithoutTimeout(ctx context.Context, log logr.Logger) context.Context {
	newCtx := context.Background()

	// Preserve logger
	newCtx = context.WithValue(newCtx, LogKey, log)

	// Preserve version if present
	if version := ctx.Value(VersionKey); version != nil {
		newCtx = context.WithValue(newCtx, VersionKey, version)
	}

	// Preserve CPU profile if present
	if cpuProfile := ctx.Value(CpuProfileKey); cpuProfile != nil {
		newCtx = context.WithValue(newCtx, CpuProfileKey, cpuProfile)
	}

	return newCtx
}
