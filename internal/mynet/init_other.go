//go:build !windows

package mynet

import "github.com/go-logr/logr"

// InitializeFirewall is a no-op on non-Windows platforms
func InitializeFirewall(logger logr.Logger) error {
	return nil
}
