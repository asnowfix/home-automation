package script

import (
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
)

func TestParseHeaterConfig(t *testing.T) {
	t.Run("nil items and nil pointers: zero value", func(t *testing.T) {
		got := parseHeaterConfig(nil, nil, nil)
		if got.EnableLogging || got.RoomID != "" || got.NormallyClosed {
			t.Errorf("expected zero-value config, got %+v", got)
		}
	})

	t.Run("all prefixed fields parsed", func(t *testing.T) {
		items := map[string]any{
			"script/heater/enable-logging":             "true",
			"script/heater/cheap-start-hour":           "22",
			"script/heater/cheap-end-hour":             "6",
			"script/heater/poll-interval-ms":           "5000",
			"script/heater/preheat-hours":              "2",
			"script/heater/internal-temperature-topic": "sensors/inside",
			"script/heater/external-temperature-topic": "sensors/outside",
		}
		got := parseHeaterConfig(items, nil, nil)

		if !got.EnableLogging {
			t.Error("expected EnableLogging true")
		}
		if got.CheapStartHour != 22 || got.CheapEndHour != 6 {
			t.Errorf("expected cheap hours 22/6, got %d/%d", got.CheapStartHour, got.CheapEndHour)
		}
		if got.PollIntervalMs != 5000 {
			t.Errorf("expected PollIntervalMs 5000, got %d", got.PollIntervalMs)
		}
		if got.PreheatHours != 2 {
			t.Errorf("expected PreheatHours 2, got %d", got.PreheatHours)
		}
		if got.InternalTemperatureTopic != "sensors/inside" || got.ExternalTemperatureTopic != "sensors/outside" {
			t.Errorf("unexpected topics: %+v", got)
		}
	})

	t.Run("room id and normally closed applied", func(t *testing.T) {
		roomID := "salon"
		normallyClosed := "true"
		got := parseHeaterConfig(nil, &roomID, &normallyClosed)
		if got.RoomID != "salon" {
			t.Errorf("expected RoomID 'salon', got %q", got.RoomID)
		}
		if !got.NormallyClosed {
			t.Error("expected NormallyClosed true")
		}
	})

	t.Run("unparseable int field left at zero", func(t *testing.T) {
		items := map[string]any{"script/heater/cheap-start-hour": "not-a-number"}
		got := parseHeaterConfig(items, nil, nil)
		if got.CheapStartHour != 0 {
			t.Errorf("expected CheapStartHour 0 on parse failure, got %d", got.CheapStartHour)
		}
	})

	t.Run("non-string item value ignored", func(t *testing.T) {
		items := map[string]any{"script/heater/enable-logging": 42}
		got := parseHeaterConfig(items, nil, nil)
		if got.EnableLogging {
			t.Error("expected EnableLogging false when stored value isn't a string")
		}
	})
}

func TestBuildHeaterKVSWrites(t *testing.T) {
	t.Run("no fields set: no writes", func(t *testing.T) {
		writes := buildHeaterKVSWrites(&myhome.HeaterSetConfigParams{})
		if len(writes) != 0 {
			t.Fatalf("expected no writes, got %+v", writes)
		}
	})

	t.Run("only fields explicitly set are written", func(t *testing.T) {
		roomID := "salon"
		enableLogging := true
		writes := buildHeaterKVSWrites(&myhome.HeaterSetConfigParams{
			RoomID:        &roomID,
			EnableLogging: &enableLogging,
		})
		if len(writes) != 2 {
			t.Fatalf("expected 2 writes, got %d: %+v", len(writes), writes)
		}
		byField := make(map[string]heaterKVSWrite)
		for _, w := range writes {
			byField[w.Field] = w
		}
		if w, ok := byField["enable_logging"]; !ok || w.Value != "true" {
			t.Errorf("expected enable_logging write with value 'true', got %+v", byField["enable_logging"])
		}
		if w, ok := byField["room_id"]; !ok || w.Value != "salon" {
			t.Errorf("expected room_id write with value 'salon', got %+v", byField["room_id"])
		}
	})

	t.Run("bool false is written as the string false", func(t *testing.T) {
		normallyClosed := false
		writes := buildHeaterKVSWrites(&myhome.HeaterSetConfigParams{NormallyClosed: &normallyClosed})
		if len(writes) != 1 || writes[0].Value != "false" {
			t.Fatalf("expected a single normally_closed write with value 'false', got %+v", writes)
		}
	})

	t.Run("int fields formatted as decimal", func(t *testing.T) {
		cheapStart, pollMs := 22, 5000
		writes := buildHeaterKVSWrites(&myhome.HeaterSetConfigParams{
			CheapStartHour: &cheapStart,
			PollIntervalMs: &pollMs,
		})
		byField := make(map[string]string)
		for _, w := range writes {
			byField[w.Field] = w.Value
		}
		if byField["cheap_start_hour"] != "22" {
			t.Errorf("expected cheap_start_hour '22', got %q", byField["cheap_start_hour"])
		}
		if byField["poll_interval_ms"] != "5000" {
			t.Errorf("expected poll_interval_ms '5000', got %q", byField["poll_interval_ms"])
		}
	})

	t.Run("writes preserve original field order", func(t *testing.T) {
		roomID := "salon"
		externalTopic := "sensors/outside"
		enableLogging := true
		writes := buildHeaterKVSWrites(&myhome.HeaterSetConfigParams{
			ExternalTemperatureTopic: &externalTopic,
			EnableLogging:            &enableLogging,
			RoomID:                   &roomID,
		})
		wantOrder := []string{"enable_logging", "room_id", "external_temperature_topic"}
		if len(writes) != len(wantOrder) {
			t.Fatalf("expected %d writes, got %d", len(wantOrder), len(writes))
		}
		for i, want := range wantOrder {
			if writes[i].Field != want {
				t.Errorf("writes[%d].Field: expected %q, got %q", i, want, writes[i].Field)
			}
		}
	})
}
