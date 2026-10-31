package roomba

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeScanner []Info

func (f fakeScanner) Scan(ctx context.Context) ([]Info, error) { return f, nil }

const pairBLID = "0123456789ABCDEF0123456789ABCDEF"

func TestPairReturnsIdentityAndPassword(t *testing.T) {
	calls := 0
	p := &Pairer{
		Scanner: fakeScanner{{BLID: pairBLID, IP: "192.0.2.50", Name: "Edmond"}},
		Password: func(ctx context.Context, host, blid string) (string, error) {
			calls++
			if host != "192.0.2.50" || blid != pairBLID {
				t.Errorf("password asked of %s/%s", host, blid)
			}
			if calls == 1 {
				return "", errors.New("connection refused") // too soon after another connection
			}
			return "secret-password-30-chars-long", nil
		},
	}
	info, pw, err := p.Pair(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if info.BLID != pairBLID || info.Name != "Edmond" || pw != "secret-password-30-chars-long" {
		t.Fatalf("paired %+v", info)
	}
	if calls != 2 {
		t.Fatalf("password calls = %d, want one retry after a transient error", calls)
	}
}

func TestPairNeedsButton(t *testing.T) {
	calls := 0
	p := &Pairer{
		Scanner: fakeScanner{{BLID: pairBLID, IP: "192.0.2.50"}},
		Password: func(context.Context, string, string) (string, error) {
			calls++
			return "", ErrNeedsButton
		},
	}
	if _, _, err := p.Pair(context.Background(), ""); !errors.Is(err, ErrNeedsButton) {
		t.Fatalf("err = %v", err)
	}
	if calls != 1 {
		t.Fatalf("retried %d times; a missing button press cannot be fixed by retrying", calls)
	}
}

func TestPairCandidates(t *testing.T) {
	never := func(context.Context, string, string) (string, error) {
		t.Error("password requested without a single candidate")
		return "", nil
	}
	if _, _, err := (&Pairer{Scanner: fakeScanner{}, Password: never}).Pair(context.Background(), ""); err == nil {
		t.Fatal("no robot: want an error")
	}
	two := fakeScanner{{BLID: "a", IP: "192.0.2.1", Name: "A"}, {BLID: "b", IP: "192.0.2.2", Name: "B"}}
	if _, _, err := (&Pairer{Scanner: two, Password: never}).Pair(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "several robots") {
		t.Fatalf("two robots: err = %v", err)
	}
	if _, _, err := (&Pairer{Scanner: fakeScanner{}, Password: never}).Pair(context.Background(), "not-an-ip"); err == nil || !strings.Contains(err.Error(), "IP address") {
		t.Fatalf("bad host: err = %v", err)
	}
}
