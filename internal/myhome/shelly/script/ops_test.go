package script

import "testing"

// TestShouldUpload covers the decision that #449 got wrong: a version marker
// matching the code is not proof the device still has the script.
func TestShouldUpload(t *testing.T) {
	const version = "abc123"

	tests := []struct {
		name       string
		force      bool
		want       string
		kvsVersion string
		present    bool
		wantUpload bool
	}{
		{
			name:       "version differs, script present",
			want:       version,
			kvsVersion: "old",
			present:    true,
			wantUpload: true,
		},
		{
			name:       "version matches and script is present: skip",
			want:       version,
			kvsVersion: version,
			present:    true,
			wantUpload: false,
		},
		{
			// The #449 case. Before the fix this skipped the upload, then tried
			// to start a script the device does not have — reporting either
			// "script not found" or, when something had recreated it empty,
			// success for a script that does nothing.
			name:       "version matches but script is gone: upload anyway",
			want:       version,
			kvsVersion: version,
			present:    false,
			wantUpload: true,
		},
		{
			name:       "force overrides a matching version",
			force:      true,
			want:       version,
			kvsVersion: version,
			present:    true,
			wantUpload: true,
		},
		{
			name:       "no marker recorded yet",
			want:       version,
			kvsVersion: "",
			present:    false,
			wantUpload: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := shouldUpload(tc.force, tc.want, tc.kvsVersion, tc.present)
			if got != tc.wantUpload {
				t.Errorf("shouldUpload(force=%v, want=%q, kvs=%q, present=%v) = %v (%q), want %v",
					tc.force, tc.want, tc.kvsVersion, tc.present, got, reason, tc.wantUpload)
			}
			if reason == "" {
				t.Error("expected a non-empty reason for the log line")
			}
		})
	}
}
