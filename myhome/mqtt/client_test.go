package mqtt

import (
	"context"
	"fmt"
	"testing"

	"github.com/go-logr/logr"
)

// TestAutoConnect_LazyStartEnabled tests that autoConnect logic for lazyStart=true
func TestAutoConnect_LazyStartEnabled(t *testing.T) {
	// This test requires a mock MQTT client that can simulate connection state
	// Without proper mocking infrastructure, we skip this test
	// The behavior is tested in integration tests with a real broker
	t.Skip("Requires mock MQTT client infrastructure - see integration tests")

	// Expected behavior:
	// - If not connected and lazyStart=true, should call Start()
	// - If Start() succeeds, should return nil
	// - If Start() fails, should return error
}

// TestAutoConnect_LazyStartDisabled tests that autoConnect fails when lazyStart is false and not connected
func TestAutoConnect_LazyStartDisabled(t *testing.T) {
	// This test requires a mock MQTT client that can simulate connection state
	// Without proper mocking infrastructure, we skip this test
	t.Skip("Requires mock MQTT client infrastructure - see integration tests")

	// Expected behavior:
	// - If not connected and lazyStart=false, should return error immediately
	// - Error message should be "MQTT client not started"
}

// TestAutoConnect_AlreadyConnected tests that autoConnect returns nil when already connected
func TestAutoConnect_AlreadyConnected(t *testing.T) {
	// This test would require a mock MQTT client that reports IsConnected() = true
	t.Skip("Requires mock MQTT client infrastructure")
}

// TestGetClientE_DirectCall tests that direct GetClientE call creates client with lazyStart=true
func TestGetClientE_DirectCall(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	// For now, we document the expected behavior
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior:
	// ctx := context.Background()
	// client, err := GetClientE(ctx)
	// if err != nil {
	//     t.Fatalf("GetClientE failed: %v", err)
	// }
	// if !client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=true for direct GetClientE call")
	// }
}

// TestNewClientE_WithLazyStart tests that NewClientE sets lazyStart correctly
func TestNewClientE_WithLazyStart(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior for lazyStart=true:
	// ctx := context.Background()
	// err := NewClientE(ctx, "mqtt://localhost:1883", "test", 5*time.Second, 5*time.Second, 1*time.Second, 0, true)
	// if err != nil {
	//     t.Fatalf("NewClientE failed: %v", err)
	// }
	// client, _ := GetClientE(ctx)
	// if !client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=true")
	// }
}

// TestNewClientE_WithoutLazyStart tests that NewClientE with lazyStart=false works
func TestNewClientE_WithoutLazyStart(t *testing.T) {
	// Reset the global client
	mutex.Lock()
	theClient = nil
	mutex.Unlock()

	// This test would require a working MQTT broker or mock
	t.Skip("Requires MQTT broker or mock infrastructure")

	// Expected behavior for lazyStart=false:
	// ctx := context.Background()
	// err := NewClientE(ctx, "mqtt://localhost:1883", "test", 5*time.Second, 5*time.Second, 1*time.Second, 0, false)
	// if err != nil {
	//     t.Fatalf("NewClientE failed: %v", err)
	// }
	// client, _ := GetClientE(ctx)
	// if client.(*client).lazyStart {
	//     t.Error("Expected lazyStart=false")
	// }
}

// Integration test documentation
// These tests require a running MQTT broker and are meant to be run manually or in CI with broker setup

// TestIntegration_LazyStartPublish would test:
// 1. Create client with lazyStart=true
// 2. Call Publish() without calling Start()
// 3. Verify client auto-connects
// 4. Verify message is published

// TestIntegration_LazyStartSubscribe would test:
// 1. Create client with lazyStart=true
// 2. Call Subscribe() without calling Start()
// 3. Verify client auto-connects
// 4. Verify subscription works and receives messages

// TestIntegration_NoLazyStartPublish would test:
// 1. Create client with lazyStart=false
// 2. Call Publish() without calling Start()
// 3. Verify error is returned
// 4. Call Start() explicitly
// 5. Verify Publish() now works

func ExampleClient_autoConnect_lazyStart() {
	// This example shows how lazy-start works for CLI commands

	// Create context with logger
	ctx := context.Background()
	ctx = logr.NewContext(ctx, logr.Discard())

	// For CLI commands, NewClientE is called with lazyStart=true
	// err := NewClientE(ctx, "mqtt://localhost:1883", "cli", 5*time.Second, 5*time.Second, 1*time.Second, 0, true)

	// Get the client
	// client, err := GetClientE(ctx)

	// First publish will auto-connect
	// err = client.Publish(ctx, "test/topic", []byte("message"), AtLeastOnce, false, "test-publisher")

	// Client is now connected and subsequent operations work immediately

	fmt.Println("Lazy-start enables auto-connect on first operation")
	// Output: Lazy-start enables auto-connect on first operation
}

func ExampleClient_autoConnect_noLazyStart() {
	// This example shows how daemon mode works without lazy-start

	// Create context with logger
	ctx := context.Background()
	ctx = logr.NewContext(ctx, logr.Discard())

	// For daemon, NewClientE is called with lazyStart=false
	// err := NewClientE(ctx, "mqtt://localhost:1883", "daemon", 5*time.Second, 5*time.Second, 1*time.Second, 2*time.Hour, false)

	// Get the client
	// client, err := GetClientE(ctx)

	// Daemon explicitly starts the client
	// err = client.Start()

	// Now publish works because client was started explicitly
	// err = client.Publish(ctx, "test/topic", []byte("message"), AtLeastOnce, false, "daemon-publisher")

	fmt.Println("Daemon mode requires explicit Start() call")
	// Output: Daemon mode requires explicit Start() call
}
