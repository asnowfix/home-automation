package http

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

// connCounter tracks how many TCP connections a test server currently holds
// open (accepted and not yet closed).
type connCounter struct{ open atomic.Int32 }

func (c *connCounter) hook(_ net.Conn, s http.ConnState) {
	switch s {
	case http.StateNew:
		c.open.Add(1)
	case http.StateClosed, http.StateHijacked:
		c.open.Add(-1)
	}
}

// TestCallE_DoesNotLeakConnections is the regression test for #695: response
// bodies that are not closed pin their keep-alive connection (and its
// persistConn goroutines): after N calls the server still holds N open
// connections even after the client closed its idle ones; with bodies closed,
// none remain.
//
// Bodies are padded with whitespace so the client never reads to EOF by
// accident (reading to EOF would release the connection even without Close).
func TestCallE_DoesNotLeakConnections(t *testing.T) {
	t.Cleanup(func() { types.SetHostResolver(nil) })
	pad := strings.Repeat(" ", 64*1024)

	cases := []struct {
		name    string
		handler http.HandlerFunc
		wantErr bool
	}{
		{"success", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `{"ok":true}`+pad)
		}, false},
		{"decode_error", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `not json`+pad)
		}, true},
		{"status_404", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope"+pad, http.StatusNotFound)
		}, true},
	}
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		for _, tc := range cases {
			t.Run(method+"/"+tc.name, func(t *testing.T) {
				http.DefaultClient.CloseIdleConnections()
				cc := &connCounter{}
				srv := httptest.NewUnstartedServer(tc.handler)
				srv.Config.ConnState = cc.hook
				srv.Start()
				defer srv.Close()
				defer http.DefaultClient.CloseIdleConnections()

				// Re-resolution is attempted on error; make it a no-op.
				types.SetHostResolver(fakeResolverFunc(func(ctx context.Context, mac net.HardwareAddr, name string) (net.IP, error) {
					return nil, errors.New("no resolver in test")
				}))

				const calls = 8
				for i := 0; i < calls; i++ {
					device := types.NewFakeDevice()
					device.IdValue = "shellyplus1pm-aabbccddeeff"
					device.HostValue = srv.Listener.Addr().String()
					verb := types.MethodHandler{Method: "Shelly.GetStatus", HttpMethod: method}
					out := map[string]any{}
					_, err := httpChannel.callE(withLogger(context.Background()), device, verb, &out, nil)
					if (err != nil) != tc.wantErr {
						t.Fatalf("call %d: err=%v, wantErr=%v", i, err, tc.wantErr)
					}
				}
				// Connections whose body was released are idle in the client
				// pool and get closed here; connections pinned by an unclosed
				// body are not idle and survive. The server notices client-side
				// closes asynchronously, hence the poll.
				http.DefaultClient.CloseIdleConnections()
				deadline := time.Now().Add(2 * time.Second)
				for cc.open.Load() > 0 && time.Now().Before(deadline) {
					time.Sleep(5 * time.Millisecond)
				}
				if got := cc.open.Load(); got > 0 {
					t.Errorf("%d connections still open after %d sequential calls: response bodies are leaking", got, calls)
				}
			})
		}
	}
}
