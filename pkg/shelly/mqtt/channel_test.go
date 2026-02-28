package mqtt

import (
	"context"
	"testing"
	"time"
)

// --- Subscribe ---

func TestMockClient_Subscribe_ReturnsChannel(t *testing.T) {
	mc := NewMockClient()
	ch, err := mc.Subscribe(context.Background(), "test/topic", 4, "subscriber")
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}
}

func TestMockClient_Subscribe_ChannelIsReadable(t *testing.T) {
	mc := NewMockClient()
	ch, err := mc.Subscribe(context.Background(), "test/topic", 4, "subscriber")
	if err != nil {
		t.Fatalf("Subscribe error: %v", err)
	}
	// Channel should not block on receive; it should be empty.
	select {
	case <-ch:
		t.Error("unexpected message on empty channel")
	default:
		// correct: channel is empty
	}
}

// --- Publish ---

func TestMockClient_Publish_NoError(t *testing.T) {
	mc := NewMockClient()
	err := mc.Publish(context.Background(), "test/topic", []byte("hello"), 0, false, "test")
	if err != nil {
		t.Errorf("Publish returned unexpected error: %v", err)
	}
}

func TestMockClient_Publish_MultipleMessages(t *testing.T) {
	mc := NewMockClient()
	for i := 0; i < 5; i++ {
		if err := mc.Publish(context.Background(), "t", []byte{byte(i)}, 0, false, "test"); err != nil {
			t.Fatalf("Publish[%d]: %v", i, err)
		}
	}
}

// --- Publisher ---

func TestMockClient_Publisher_DrainsSafely(t *testing.T) {
	mc := NewMockClient()
	ch, err := mc.Publisher(context.Background(), "test/topic", 4, 0, false, "pub")
	if err != nil {
		t.Fatalf("Publisher error: %v", err)
	}
	if ch == nil {
		t.Fatal("expected non-nil send channel")
	}

	// Send several messages; they should be drained without blocking.
	for i := 0; i < 3; i++ {
		ch <- []byte("msg")
	}
	// Give the drain goroutine a moment.
	time.Sleep(20 * time.Millisecond)
	// If we reach here without deadlock, the test passes.
}

// --- SubscribeWithHandler ---

func TestMockClient_SubscribeWithHandler_NoError(t *testing.T) {
	mc := NewMockClient()
	err := mc.SubscribeWithHandler(
		context.Background(),
		"test/topic",
		4,
		"subscriber",
		func(topic string, payload []byte, subscriber string) error { return nil },
	)
	if err != nil {
		t.Errorf("SubscribeWithHandler returned unexpected error: %v", err)
	}
}

// --- Identity methods ---

func TestMockClient_GetServer(t *testing.T) {
	mc := NewMockClient()
	if mc.GetServer() == "" {
		t.Error("expected non-empty server string")
	}
}

func TestMockClient_Id(t *testing.T) {
	mc := NewMockClient()
	if mc.Id() == "" {
		t.Error("expected non-empty client ID")
	}
}

func TestMockClient_BrokerUrl(t *testing.T) {
	mc := NewMockClient()
	u := mc.BrokerUrl()
	if u == nil {
		t.Error("expected non-nil BrokerUrl")
	}
}
