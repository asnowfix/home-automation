package tools

import "testing"

func TestNormalizeMac(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		// Already canonical
		{"e8:e0:7e:a6:0c:6f", "e8:e0:7e:a6:0c:6f"},
		// Uppercase colon-separated
		{"E8:E0:7E:A6:0C:6F", "e8:e0:7e:a6:0c:6f"},
		// Plain hex (no separators)
		{"E8E07EA60C6F", "e8:e0:7e:a6:0c:6f"},
		{"e8e07ea60c6f", "e8:e0:7e:a6:0c:6f"},
		// Dash-separated
		{"e8-e0-7e-a6-0c-6f", "e8:e0:7e:a6:0c:6f"},
		{"E8-E0-7E-A6-0C-6F", "e8:e0:7e:a6:0c:6f"},
		// Real device MACs seen in the field
		{"b0c7de1158d5", "b0:c7:de:11:58:d5"},
		{"e8e07ed0f989", "e8:e0:7e:d0:f9:89"},
		// Non-MAC strings pass through lowercased/trimmed
		{"hello", "hello"},
		{"  HELLO  ", "hello"},
		// Empty
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got := NormalizeMac(tt.in)
			if got != tt.want {
				t.Errorf("NormalizeMac(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
