package rooms

import (
	"fmt"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
	"github.com/jmoiron/sqlx"
)

// newBenchDB opens an in-memory SQLite database with logging silenced.
func newBenchDB(b *testing.B) *sqlx.DB {
	b.Helper()
	db, err := sqlx.Connect("sqlite", ":memory:")
	if err != nil {
		b.Fatalf("sqlx.Connect: %v", err)
	}
	b.Cleanup(func() { db.Close() })
	return db
}

// newBenchStorage wires a Storage to an in-memory database.
func newBenchStorage(b *testing.B) *Storage {
	b.Helper()
	s, err := NewStorage(logr.Discard(), newBenchDB(b))
	if err != nil {
		b.Fatalf("NewStorage: %v", err)
	}
	return s
}

var (
	benchKinds    = []myhome.RoomKind{myhome.RoomKindBedroom, myhome.RoomKindOffice, myhome.RoomKindLivingRoom, myhome.RoomKindKitchen, myhome.RoomKindOther}
	benchDayTypes = []myhome.DayType{myhome.DayTypeWorkDay, myhome.DayTypeDayOff}
	benchRanges   = []myhome.TemperatureTimeRange{{Start: 360, End: 1380}}
)

// BenchmarkSaveRoom_Insert cycles through a pool of 20 room IDs (realistic
// max for a house).  After the first 20 iterations the operation becomes an
// upsert, keeping the database bounded and safe for large b.N.
func BenchmarkSaveRoom_Insert(b *testing.B) {
	const poolSize = 20
	s := newBenchStorage(b)
	b.ResetTimer()
	for i := range b.N {
		config := &RoomConfig{
			ID:     fmt.Sprintf("room-%d", i%poolSize),
			Name:   fmt.Sprintf("Room %d", i%poolSize),
			Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
			Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
		}
		if _, err := s.SaveRoom(config); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSaveRoom_NoChange measures the hot no-op upsert path for rooms.
func BenchmarkSaveRoom_NoChange(b *testing.B) {
	s := newBenchStorage(b)
	config := &RoomConfig{
		ID:     "bench-room",
		Name:   "Bench Room",
		Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	}
	if _, err := s.SaveRoom(config); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.SaveRoom(config); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetRoom measures a primary-key point lookup on temperature_rooms.
func BenchmarkGetRoom(b *testing.B) {
	s := newBenchStorage(b)
	config := &RoomConfig{
		ID:     "bench-room",
		Name:   "Bench Room",
		Kinds:  []myhome.RoomKind{myhome.RoomKindLivingRoom},
		Levels: map[string]float64{"eco": 17.0, "comfort": 21.0},
	}
	if _, err := s.SaveRoom(config); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.GetRoom("bench-room"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkListRooms_10 measures a full table scan with 10 pre-loaded rooms.
func BenchmarkListRooms_10(b *testing.B) {
	s := newBenchStorage(b)
	for i := range 10 {
		_, err := s.SaveRoom(&RoomConfig{
			ID:     fmt.Sprintf("room-%d", i),
			Name:   fmt.Sprintf("Room %d", i),
			Kinds:  []myhome.RoomKind{myhome.RoomKindOther},
			Levels: map[string]float64{"eco": 17.0},
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.ListRooms(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSetKindSchedule_Upsert cycles through all 10 (kind, day_type) slots,
// pre-populated so each iteration exercises the ON CONFLICT UPDATE path.
func BenchmarkSetKindSchedule_Upsert(b *testing.B) {
	s := newBenchStorage(b)
	for _, k := range benchKinds {
		for _, dt := range benchDayTypes {
			if _, err := s.SetKindSchedule(k, dt, benchRanges); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ResetTimer()
	for i := range b.N {
		k := benchKinds[i%len(benchKinds)]
		dt := benchDayTypes[i%len(benchDayTypes)]
		if _, err := s.SetKindSchedule(k, dt, benchRanges); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetKindSchedules_All retrieves all 10 kind schedules without a filter.
func BenchmarkGetKindSchedules_All(b *testing.B) {
	s := newBenchStorage(b)
	for _, k := range benchKinds {
		for _, dt := range benchDayTypes {
			if _, err := s.SetKindSchedule(k, dt, benchRanges); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.GetKindSchedules(nil, nil); err != nil {
			b.Fatal(err)
		}
	}
}
