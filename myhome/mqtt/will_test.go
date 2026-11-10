package mqtt

import (
	"fmt"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	mochiServer "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/hooks/auth"
	"github.com/mochi-mqtt/server/v2/listeners"
)

// freshOptions swaps the package-level connect options (and singleton) for
// pristine ones for the duration of the test.
func freshOptions(t *testing.T) {
	t.Helper()
	savedOps, savedClient := mqttOps, theClient
	mqttOps, theClient = mqtt.NewClientOptions(), nil
	t.Cleanup(func() { mqttOps, theClient = savedOps, savedClient })
}

func TestSetWill_NotConfiguredByDefault(t *testing.T) {
	freshOptions(t)
	r := mqtt.NewClient(mqttOps).OptionsReader()
	if r.WillEnabled() {
		t.Fatal("no will must be set when SetWill was never called")
	}
}

func TestSetWill_AppliedToConnectOptions(t *testing.T) {
	freshOptions(t)
	payload := []byte(`{"online":false}`)
	if err := SetWill(Will{Topic: "vacuum/x/availability", Payload: payload, QoS: 1, Retained: true}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X' // caller mutating its slice must not alter the will

	r := mqtt.NewClient(mqttOps).OptionsReader()
	if !r.WillEnabled() || r.WillTopic() != "vacuum/x/availability" ||
		string(r.WillPayload()) != `{"online":false}` || r.WillQos() != 1 || !r.WillRetained() {
		t.Fatalf("unexpected will: enabled=%v topic=%q payload=%q qos=%d retained=%v",
			r.WillEnabled(), r.WillTopic(), r.WillPayload(), r.WillQos(), r.WillRetained())
	}
}

func TestSetWill_Validation(t *testing.T) {
	freshOptions(t)
	if err := SetWill(Will{}); err == nil {
		t.Error("empty topic must be rejected")
	}
	if err := SetWill(Will{Topic: "t", QoS: 3}); err == nil {
		t.Error("QoS 3 must be rejected")
	}
	r := mqtt.NewClient(mqttOps).OptionsReader()
	if r.WillEnabled() {
		t.Error("rejected wills must not be applied")
	}
	theClient = &client{}
	if err := SetWill(Will{Topic: "t"}); err == nil {
		t.Error("SetWill after client creation must fail")
	}
}

func TestRecordingMock_RecordsWill(t *testing.T) {
	m := NewRecordingMockClient()
	if _, ok := m.Will(); ok {
		t.Fatal("mock must report no will by default")
	}
	if err := m.SetWill(Will{Topic: "a/b", Payload: []byte("p"), QoS: 1, Retained: true}); err != nil {
		t.Fatal(err)
	}
	w, ok := m.Will()
	if !ok || w.Topic != "a/b" || string(w.Payload) != "p" || w.QoS != 1 || !w.Retained {
		t.Fatalf("unexpected recorded will: %+v ok=%v", w, ok)
	}
	if err := m.SetWill(Will{}); err == nil {
		t.Error("mock must validate like the real SetWill")
	}
	m.Reset()
	if _, ok := m.Will(); ok {
		t.Error("Reset must clear the will")
	}
}

// TestSetWill_DeliveredOnEveryConnect runs against an in-process broker: the
// will is published when the connection drops, the client auto-reconnects, and
// the will is published again when the second connection drops.
func TestSetWill_DeliveredOnEveryConnect(t *testing.T) {
	freshOptions(t)

	srv := mochiServer.New(&mochiServer.Options{InlineClient: true})
	if err := srv.AddHook(new(auth.AllowHook), nil); err != nil {
		t.Fatal(err)
	}
	l := listeners.NewTCP(listeners.Config{ID: "t", Address: "127.0.0.1:0"})
	if err := srv.AddListener(l); err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Close() })

	// Wait for the listener to be bound to read its port.
	var addr string
	for i := 0; i < 100 && (addr == "" || addr == "127.0.0.1:0"); i++ {
		time.Sleep(20 * time.Millisecond)
		addr = l.Address()
	}
	broker := "tcp://" + addr

	got := make(chan string, 4)
	w := mqtt.NewClient(mqtt.NewClientOptions().AddBroker(broker).SetClientID("watcher"))
	if tok := w.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("watcher connect: %v", tok.Error())
	}
	t.Cleanup(func() { w.Disconnect(100) })
	if tok := w.Subscribe("vac/+/availability", 1, func(_ mqtt.Client, m mqtt.Message) {
		got <- string(m.Payload())
	}); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("watcher subscribe: %v", tok.Error())
	}

	if err := SetWill(Will{Topic: "vac/1/availability", Payload: []byte(`{"online":false}`), QoS: 1, Retained: true}); err != nil {
		t.Fatal(err)
	}
	connects := make(chan struct{}, 4)
	mqttOps.AddBroker(broker).SetClientID("owner").SetAutoReconnect(true).
		SetConnectRetryInterval(100 * time.Millisecond).
		SetOnConnectHandler(func(mqtt.Client) { connects <- struct{}{} })
	c := mqtt.NewClient(mqttOps)
	if tok := c.Connect(); !tok.WaitTimeout(5*time.Second) || tok.Error() != nil {
		t.Fatalf("connect: %v", tok.Error())
	}
	t.Cleanup(func() { c.Disconnect(100) })
	<-connects

	for round := 1; round <= 2; round++ {
		cl, ok := srv.Clients.Get("owner")
		if !ok {
			t.Fatalf("round %d: owner not connected", round)
		}
		cl.Stop(fmt.Errorf("simulated crash %d", round)) // ungraceful: broker must publish the will
		select {
		case p := <-got:
			if p != `{"online":false}` {
				t.Fatalf("round %d: unexpected will payload %q", round, p)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("round %d: will not delivered", round)
		}
		if round == 1 {
			select {
			case <-connects:
			case <-time.After(10 * time.Second):
				t.Fatal("client did not reconnect")
			}
		}
	}
}
