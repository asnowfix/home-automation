package script

import "testing"

func TestUploadStatus_String(t *testing.T) {
	tests := []struct {
		s    UploadStatus
		want string
	}{
		{StatusUploaded, "uploaded"},
		{StatusIndeterminate, "indeterminate"},
		{StatusFailed, "failed"},
		{UploadStatus(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.s.String(); got != tt.want {
			t.Errorf("UploadStatus(%d).String() = %q, want %q", int(tt.s), got, tt.want)
		}
	}
}
