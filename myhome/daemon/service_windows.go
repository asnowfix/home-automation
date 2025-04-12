//go:build windows
// +build windows

package daemon

import (
	"context"
	"fmt"
	"hlog"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"github.com/go-logr/logr"
)

type myhomeService struct {
	run    func(context.Context, logr.Logger) error
	cancel context.CancelFunc
	ctx    context.Context
	wg     sync.WaitGroup
	log    logr.Logger
	elog   debug.Log
}

func isInteractive(foreground bool) bool {
	if foreground {
		return true
	}
	isService, err := svc.IsWindowsService()
	if err != nil {
		return false
	}
	return !isService
}

func NewService(ctx context.Context, cancel context.CancelFunc, run func(context.Context, logr.Logger) error) *myhomeService {
	return &myhomeService{
		ctx:    ctx,
		cancel: cancel,
		run:    run,
		log:    hlog.Logger,
	}
}

// Execute is called by Windows Service Control Manager
func (m *myhomeService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	// Report start pending with a timeout
	changes <- svc.Status{State: svc.StartPending, WaitHint: 30 * 1000}

	// Start main service logic
	errChan := make(chan error, 1)
	m.wg.Add(1)

	go func() {
		defer m.wg.Done()
		if err := m.run(m.ctx, m.log); err != nil {
			m.elog.Error(2, fmt.Sprintf("Service failed: %v", err))
			errChan <- err
		}
		close(errChan)
	}()

	// Wait briefly to see if the service starts successfully
	select {
	case err := <-errChan:
		if err != nil {
			m.log.Error(err, "Service failed to start")
			return false, 1
		}
	case <-time.After(1 * time.Second):
		// Service started without immediate error
	}

	// Report running state
	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}
	m.elog.Info(1, "Service started successfully")

	// Service loop
	for {
		select {
		case err := <-errChan:
			if err != nil {
				m.log.Error(err, "Service encountered an error")
				return false, 1
			}
			return false, 0
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: uint32(10 * 1000)}
				m.cancel()

				// Wait with timeout
				done := make(chan struct{})
				go func() {
					m.wg.Wait()
					close(done)
				}()

				select {
				case <-done:
					m.elog.Info(1, "Service stopped gracefully")
				case <-time.After(10 * time.Second):
					m.elog.Warning(1, "Service stop timed out")
				}
				return
			default:
				m.elog.Error(1, fmt.Sprintf("Unexpected control request #%d", c))
			}
		}
	}
}

func (mhs *myhomeService) Run(foreground bool) error {
	if isInteractive(foreground) {
		return mhs.runInForeground()
	} else {
		return mhs.runInBackground()
	}
}

func (mhs *myhomeService) runInBackground() error {
	var err error

	// Initialize event logger first
	mhs.elog, err = eventlog.Open("MyHome")
	if err != nil {
		mhs.log.Error(err, "Failed to open event log")
		return fmt.Errorf("failed to open event log: %v", err)
	}
	defer mhs.elog.Close()

	mhs.log.Info("Running in background (creating Windows Service)")

	// svc.Run blocks until the service is stopped
	if err := svc.Run("MyHome", mhs); err != nil {
		mhs.log.Error(err, "Service execution failed")
		return fmt.Errorf("service execution failed: %v", err)
	}
	return nil
}

func (mhs *myhomeService) runInForeground() error {
	mhs.log.Info("Running in foreground")
	return mhs.run(mhs.ctx, mhs.log)
}
