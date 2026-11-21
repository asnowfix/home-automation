package mqtt

import (
	"context"
	"errors"
	"testing"
	"time"

	pahomqtt "github.com/eclipse/paho.mqtt.golang"
)

// scriptedToken is a paho token whose completion and error the test controls.
type scriptedToken struct {
	done bool
	err  error
}

func (t *scriptedToken) Wait() bool                     { return t.done }
func (t *scriptedToken) WaitTimeout(time.Duration) bool { return t.done }
func (t *scriptedToken) Done() <-chan struct{}          { return make(chan struct{}) }
func (t *scriptedToken) Error() error                   { return t.err }

func TestPublish_Outcomes(t *testing.T) {
	broker := errors.New("broker refused")
	tests := []struct {
		name    string
		token   pahomqtt.Token
		wantErr error // errors.Is target; nil means success
	}{
		{"Acknowledged", &scriptedToken{done: true}, nil},
		{"CompletedWithError", &scriptedToken{done: true, err: broker}, broker},
		{"TimeoutHasNilTokenError", &scriptedToken{done: false}, ErrPublishTimeout},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t)
			c.timeout = time.Millisecond
			c.mqtt.(*fakePahoClient).publishToken = tc.token
			err := c.Publish(context.Background(), "t/x", []byte("m"), 1, false, "test")
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil) != (err == nil) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
