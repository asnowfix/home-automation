package storage

import (
	"context"
	"fmt"
	"testing"

	"github.com/asnowfix/home-automation/internal/myhome"
	"github.com/go-logr/logr"
)

// newBenchStorage opens an in-memory database with logging silenced.
// In-memory avoids disk I/O noise so the numbers reflect pure driver/SQL overhead.
func newBenchStorage(b *testing.B) *DeviceStorage {
	b.Helper()
	s, err := NewDeviceStorage(logr.Discard(), ":memory:")
	if err != nil {
		b.Fatalf("NewDeviceStorage: %v", err)
	}
	b.Cleanup(s.Close)
	return s
}

// newFileBenchStorage opens a file-based database in a temp directory.
// WAL mode only activates on real files; use these to measure the
// PRAGMA journal_mode=WAL effect on write throughput.
func newFileBenchStorage(b *testing.B) *DeviceStorage {
	b.Helper()
	s, err := NewDeviceStorage(logr.Discard(), b.TempDir()+"/bench.db")
	if err != nil {
		b.Fatalf("NewDeviceStorage: %v", err)
	}
	b.Cleanup(s.Close)
	return s
}

func benchDevice(i int) *myhome.Device {
	return makeDevice(
		"Shelly",
		fmt.Sprintf("bench-%06d", i),
		fmt.Sprintf("aa:bb:cc:%02x:%02x:%02x", (i>>16)&0xff, (i>>8)&0xff, i&0xff),
		fmt.Sprintf("device-%d", i),
		fmt.Sprintf("192.168.%d.%d", (i/253)%254+1, i%253+1),
	)
}

// BenchmarkSetDevice_Insert cycles through a pool of 200 device IDs.
// After the first 200 iterations the operation becomes an upsert; the database
// stays bounded so the benchmark is safe for large b.N on both ncruces (wasm
// heap limited) and modernc drivers.
func BenchmarkSetDevice_Insert(b *testing.B) {
	const poolSize = 200
	s := newBenchStorage(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := range b.N {
		if _, err := s.SetDevice(ctx, benchDevice(i%poolSize), false); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSetDevice_Upsert_NoChange measures the hot no-op upsert path:
// same data, no field changed, ON CONFLICT WHERE clause should skip the update.
// Device has no MAC so the secondary UPDATE-by-MAC path does not fire.
func BenchmarkSetDevice_Upsert_NoChange(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	d := makeDevice("Shelly", "bench-fixed", "", "fixed-light", "192.168.1.1")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.SetDevice(ctx, d, true); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetDeviceById measures a composite-primary-key point lookup.
func BenchmarkGetDeviceById(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	const n = 20
	for i := range n {
		if _, err := s.SetDevice(ctx, benchDevice(i), false); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := range b.N {
		if _, err := s.GetDeviceById(ctx, fmt.Sprintf("bench-%06d", i%n)); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetDeviceByMAC measures a UNIQUE-index lookup by MAC address.
func BenchmarkGetDeviceByMAC(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	const n = 20
	for i := range n {
		if _, err := s.SetDevice(ctx, benchDevice(i), false); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := range b.N {
		j := i % n
		mac := fmt.Sprintf("aa:bb:cc:%02x:%02x:%02x", (j>>16)&0xff, (j>>8)&0xff, j&0xff)
		if _, err := s.GetDeviceByMAC(ctx, mac); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGetAllDevices_20 measures a full table scan with 20 pre-loaded rows.
func BenchmarkGetAllDevices_20(b *testing.B) {
	s := newBenchStorage(b)
	ctx := context.Background()
	for i := range 20 {
		if _, err := s.SetDevice(ctx, benchDevice(i), false); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.GetAllDevices(ctx); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkSetDevice_File_Insert and BenchmarkSetDevice_File_Upsert use a
// real file so that PRAGMA journal_mode=WAL takes effect.
// Run before and after adding the WAL pragma to NewDeviceStorage to see
// the impact on write throughput.
func BenchmarkSetDevice_File_Insert(b *testing.B) {
	const poolSize = 200
	s := newFileBenchStorage(b)
	ctx := context.Background()
	b.ResetTimer()
	for i := range b.N {
		if _, err := s.SetDevice(ctx, benchDevice(i%poolSize), false); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSetDevice_File_Upsert_NoChange(b *testing.B) {
	s := newFileBenchStorage(b)
	ctx := context.Background()
	d := makeDevice("Shelly", "bench-fixed", "", "fixed-light", "192.168.1.1")
	if _, err := s.SetDevice(ctx, d, false); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for range b.N {
		if _, err := s.SetDevice(ctx, d, true); err != nil {
			b.Fatal(err)
		}
	}
}
