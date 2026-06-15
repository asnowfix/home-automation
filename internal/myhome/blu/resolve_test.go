package blu

import (
	"testing"
)

func TestMacFromBluDeviceID(t *testing.T) {
	tests := []struct {
		name     string
		deviceID string
		want     string
		wantErr  bool
	}{
		{
			name:     "generic shellyblu prefix",
			deviceID: "shellyblu-e8e07ea60c6f",
			want:     "e8:e0:7e:a6:0c:6f",
		},
		{
			name:     "motion sensor (shellyblumotion1)",
			deviceID: "shellyblumotion1-e8e07ed0f989",
			want:     "e8:e0:7e:d0:f9:89",
		},
		{
			name:     "motion sensor parking (shellyblumotion1)",
			deviceID: "shellyblumotion1-b0c7de1158d5",
			want:     "b0:c7:de:11:58:d5",
		},
		{
			name:     "button (shellyblubutton1)",
			deviceID: "shellyblubutton1-aabbcc112233",
			want:     "aa:bb:cc:11:22:33",
		},
		{
			name:    "non-BLU Shelly device",
			deviceID: "shellypm-aabbcc112233",
			wantErr: true,
		},
		{
			name:    "shelly pro2",
			deviceID: "shellypro2-2cbcbb9fb834",
			wantErr: true,
		},
		{
			name:    "truncated MAC in device ID",
			deviceID: "shellyblumotion1-e8e07e",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := macFromBluDeviceID(tt.deviceID)
			if (err != nil) != tt.wantErr {
				t.Fatalf("macFromBluDeviceID(%q) error = %v, wantErr %v", tt.deviceID, err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("macFromBluDeviceID(%q) = %q, want %q", tt.deviceID, got, tt.want)
			}
		})
	}
}

func TestIsValidMac(t *testing.T) {
	tests := []struct {
		mac  string
		want bool
	}{
		{"e8:e0:7e:a6:0c:6f", true},
		{"b0:c7:de:11:58:d5", true},
		{"aa:bb:cc:11:22:33", true},
		{"e8e07ea60c6f", false},    // no colons
		{"e8-e0-7e-a6-0c-6f", false}, // dashes
		{"e8:e0:7e:a6:0c", false},  // too short
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.mac, func(t *testing.T) {
			if got := isValidMac(tt.mac); got != tt.want {
				t.Errorf("isValidMac(%q) = %v, want %v", tt.mac, got, tt.want)
			}
		})
	}
}
