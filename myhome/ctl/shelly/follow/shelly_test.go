package follow

import "testing"

func TestBuildShellyFollowPayload(t *testing.T) {
	tests := []struct {
		name    string
		opts    shellyFollowOptions
		want    map[string]any
		wantErr bool
	}{
		{
			name: "defaults: full mode, no auto-off",
			opts: shellyFollowOptions{
				switchID: "switch:0",
				followID: "switch:0",
			},
			want: map[string]any{
				"switch_id":   "switch:0",
				"follow_id":   "switch:0",
				"follow_mode": "full",
			},
		},
		{
			name: "activation-only via --auto-off",
			opts: shellyFollowOptions{
				switchID:   "switch:0",
				followID:   "switch:1",
				autoOff:    300,
				autoOffSet: true,
			},
			want: map[string]any{
				"switch_id":   "switch:0",
				"follow_id":   "switch:1",
				"follow_mode": "activation-only",
				"auto_off":    300,
			},
		},
		{
			name: "--auto-off=0 does not trigger activation-only",
			opts: shellyFollowOptions{
				switchID:   "switch:0",
				followID:   "switch:0",
				autoOff:    0,
				autoOffSet: true,
			},
			want: map[string]any{
				"switch_id":   "switch:0",
				"follow_id":   "switch:0",
				"follow_mode": "full",
			},
		},
		{
			name: "explicit --follow-mode full",
			opts: shellyFollowOptions{
				switchID:      "switch:0",
				followID:      "switch:0",
				followMode:    "full",
				followModeSet: true,
			},
			want: map[string]any{
				"switch_id":   "switch:0",
				"follow_id":   "switch:0",
				"follow_mode": "full",
			},
		},
		{
			name: "explicit --follow-mode activation-only with auto-off",
			opts: shellyFollowOptions{
				switchID:      "switch:0",
				followID:      "switch:0",
				followMode:    "activation-only",
				followModeSet: true,
				autoOff:       60,
				autoOffSet:    true,
			},
			want: map[string]any{
				"switch_id":   "switch:0",
				"follow_id":   "switch:0",
				"follow_mode": "activation-only",
				"auto_off":    60,
			},
		},
		{
			name: "invalid follow-mode returns error",
			opts: shellyFollowOptions{
				followMode:    "mirror",
				followModeSet: true,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildShellyFollowPayload(tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("buildShellyFollowPayload() error = %v, wantErr %v", err, tt.wantErr)
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
					t.Errorf("unexpected key %q in payload", k)
				}
			}
		})
	}
}
