//go:build !windows
// +build !windows

package daemon

import (
	"context"
	"hlog"
	"sync"

	"github.com/go-logr/logr"
)

type myhomeService struct {
	run    func(context.Context, logr.Logger) error
	cancel context.CancelFunc
	ctx    context.Context
	wg     sync.WaitGroup
}

func NewService(ctx context.Context, cancel context.CancelFunc, run func(context.Context, logr.Logger) error) *myhomeService {
	return &myhomeService{
		ctx:    ctx,
		cancel: cancel,
		run:    run,
	}
}

func (mhs *myhomeService) Run(foreground bool) error {
	hlog.Init(false)
	return mhs.run(mhs.ctx, hlog.Logger)
}
