//go:build windows
// +build windows

package daemon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"hlog"

	"github.com/kardianos/service"
	"github.com/spf13/cobra"
	"gopkg.in/natefinch/lumberjack.v2"
)

type App struct {
	cmd    *cobra.Command
	cancel context.CancelFunc
	ctx    context.Context
	wg     sync.WaitGroup
}

func (a *App) Run() error {
	if !service.Interactive() {
		// Setup file logging when running as a service
		if err := setupServiceLogging(); err != nil {
			return fmt.Errorf("failed to setup logging: %v", err)
		}
	}

	// Create a cancellable context
	a.ctx, a.cancel = context.WithCancel(context.Background())
	a.cmd.SetContext(a.ctx)

	// Run the command in a goroutine
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		if err := a.cmd.Execute(); err != nil {
			hlog.Logger.Error(err, "Command execution failed")
		}
	}()

	return nil
}

func (a *App) Stop() error {
	if a.cancel != nil {
		a.cancel()  // Signal all goroutines to stop
		a.wg.Wait() // Wait for all goroutines to finish
	}
	return nil
}

func setupServiceLogging() error {
	// Create logs directory in ProgramData instead of Program Files
	logDir := filepath.Join("C:", "ProgramData", "MyHome", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %v", err)
	}

	// Setup rotating logger
	logger := &lumberjack.Logger{
		Filename:   filepath.Join(logDir, "myhome.log"),
		MaxSize:    10,   // megabytes after which new file is created
		MaxBackups: 5,    // number of backups to keep
		MaxAge:     28,   // days to keep old logs
		Compress:   true, // compress old log files
	}

	// Initialize logger with rotating file output
	hlog.InitWithWriter(true, logger)
	return nil
}

func runAsService(app *App) error {
	svcConfig := &service.Config{
		Name:        "MyHome",
		DisplayName: "MyHome Automation Service",
		Description: "Home automation service for controlling Shelly devices",
	}

	prg := &program{app: app}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		return err
	}

	return s.Run()
}

type program struct {
	app *App
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.app.Run()
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.app != nil {
		return p.app.Stop()
	}
	return nil
}
