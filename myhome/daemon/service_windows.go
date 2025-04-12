//go:build windows
// +build windows

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/debug"
	"golang.org/x/sys/windows/svc/eventlog"

	"hlog"

	"github.com/go-logr/logr"
	"gopkg.in/natefinch/lumberjack.v2"
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
	}
}

// Execute is called when the service is started by svc.Run()
func (m *myhomeService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (ssec bool, errno uint32) {
	const cmdsAccepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	// Start main service logic
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		if err := m.run(m.ctx, m.log); err != nil {
			m.elog.Error(2, fmt.Sprintf("Service failed: %v", err))
		}
	}()

	changes <- svc.Status{State: svc.Running, Accepts: cmdsAccepted}

	// Service loop
	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
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

func setupServiceLogging() error {
	// Create logs directory in ProgramData
	logDir := filepath.Join("C:", "ProgramData", "MyHome", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Setup rotating logger
	logger := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "myhome.log"),
		MaxSize:    10, // megabytes
		MaxBackups: 5,  // number of backups
		MaxAge:     28, // days
		Compress:   true,
	}

	// Initialize logger
	hlog.InitWithWriter(true, logger)
	return nil
}

func (mhs *myhomeService) runInBackground() error {
	var err error

	// Setup logging
	if err = setupServiceLogging(); err != nil {
		mhs.elog.Error(1, fmt.Sprintf("Failed to setup logging: %v", err))
		return err
	}
	mhs.log = hlog.Logger

	// Running as a service
	mhs.elog, err = eventlog.Open("MyHome")
	if err != nil {
		return err
	}
	defer mhs.elog.Close()

	// svc.Run will create the necessary channels and call Execute
	return svc.Run("MyHome", mhs)
}

func (mhs *myhomeService) runInForeground() error {
	// When running in foreground, pass nil channels to Execute
	// This simulates running as a service but without Windows service control
	_, errno := mhs.Execute(nil, nil, nil)
	if errno != 0 {
		return fmt.Errorf("service execution failed with error %d", errno)
	}
	return nil
}
