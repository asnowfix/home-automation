package follow

import "testing"

func TestBuildBluFollowPayload(t *testing.T) {
	tests := []struct {
		name    string
		opts    bluFollowOptions
		want    map[string]any
		wantErr bool
	}{
		{
			name: "defaults: switch:0, auto_off=300, illuminance_max=10%",
			opts: bluFollowOptions{
				switchID: "switch:0",
				autoOff:  300,
			},
			want: map[string]any{
				"switch_id":      "switch:0",
				"auto_off":       300,
				"illuminance_max": "10%",
			},
		},
		{
			name: "explicit switch:1",
			opts: bluFollowOptions{
				switchID: "switch:1",
				autoOff:  300,
			},
			want: map[string]any{
				"switch_id":      "switch:1",
				"auto_off":       300,
				"illuminance_max": "10%",
			},
		},
		{
			name: "numeric illuminance_max overrides default",
			opts: bluFollowOptions{
				switchID:    "switch:0",
				autoOff:     300,
				illumMax:    "50",
				illumMaxSet: true,
			},
			want: map[string]any{
				"switch_id":      "switch:0",
				"auto_off":       300,
				"illuminance_max": 50,
			},
		},
		{
			name: "percentage illuminance_max",
			opts: bluFollowOptions{
				switchID:    "switch:0",
				autoOff:     300,
				illumMax:    "20%",
				illumMaxSet: true,
			},
			want: map[string]any{
				"switch_id":      "switch:0",
				"auto_off":       300,
				"illuminance_max": "20%",
			},
		},
		{
			name: "illuminance_min set",
			opts: bluFollowOptions{
				switchID:    "switch:0",
				autoOff:     300,
				illumMin:    "5",
				illumMinSet: true,
			},
			want: map[string]any{
				"switch_id":      "switch:0",
				"auto_off":       300,
				"illuminance_min": 5,
				"illuminance_max": "10%",
			},
		},
		{
			name: "next_switch set",
			opts: bluFollowOptions{
				switchID:      "switch:0",
				autoOff:       300,
				nextSwitch:    "switch:1",
				nextSwitchSet: true,
			},
			want: map[string]any{
				"switch_id":      "switch:0",
				"auto_off":       300,
				"illuminance_max": "10%",
				"next_switch":    "switch:1",
			},
		},
		{
			name: "invalid illuminance_min returns error",
			opts: bluFollowOptions{
				illumMin:    "notanumber",
				illumMinSet: true,
			},
			wantErr: true,
		},
		{
			name: "percentage out of range returns error",
			opts: bluFollowOptions{
				illumMax:    "150%",
				illumMaxSet: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildBluFollowPayload(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildBluFollowPayload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			for k, wantV := range tt.want {
				gotV, ok := got[k]
				if !ok {
					t.Errorf("missing key %q in payload", k)
					continue
				}
				if gotV != wantV {
					t.Errorf("payload[%q] = %v (%T), want %v (%T)", k, gotV, gotV, wantV, wantV)
				}
			}
			for k := range got {
				if _, ok := tt.want[k]; !ok {
					t.Errorf("unexpected key %q in payload (value: %v)", k, got[k])
				}
			}
		})
	}
}

func TestValidateIlluminanceValue(t *testing.T) {
	tests := []struct {
		value   string
		wantErr bool
	}{
		{"", false},
		{"0", false},
		{"100", false},
		{"50.5", false},
		{"0%", false},
		{"100%", false},
		{"20%", false},
		{"101%", true},
		{"-1%", true},
		{"abc", true},
		{"20x", true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			err := validateIlluminanceValue(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateIlluminanceValue(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
		})
	}
}
