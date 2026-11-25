// prepare_test.go — #897: PrepareCode is the one definition of what the
// uploader sends, shared with the version-marker backfill.
package script

import (
	"context"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/pkg/shelly/types"
)

// A source with a plain template literal (downgraded by PrepareCode) and
// enough statements to need several PutCode chunks once minified.
func prepareSource() string {
	var b strings.Builder
	b.WriteString("var greeting = `hello\nworld`;\n")
	for i := 0; i < 400; i++ {
		b.WriteString("function f")
		b.WriteString(strings.Repeat("a", i%7+1))
		b.WriteString("(x) { return x + 1; /* comment */ }\n")
	}
	return b.String()
}

func uploadedCode(t *testing.T, src []byte, minify bool) (string, int) {
	t.Helper()
	var sent strings.Builder
	chunks := 0
	dev := &fakeDevice{callFn: func(method string, params any) (any, error) {
		switch method {
		case List.String():
			return &ListResponse{Scripts: nil}, nil
		case Create.String():
			return &Status{Id: 1}, nil
		case PutCode.String():
			chunks++
			sent.WriteString(params.(*PutCodeRequest).Code)
			return &PutCodeResponse{}, nil
		case SetConfig.String():
			return &Status{Id: 1}, nil
		}
		t.Fatalf("unexpected RPC call %s", method)
		return nil, nil
	}}
	if _, err := Upload(context.Background(), types.ChannelDefault, dev, "prepare.js", src, minify); err != nil {
		t.Fatalf("Upload: %v", err)
	}
	return sent.String(), chunks
}

func TestPrepareCode_EqualsWhatUploadSends(t *testing.T) {
	src := []byte(prepareSource())
	for _, minify := range []bool{true, false} {
		want, err := PrepareCode(src, minify)
		if err != nil {
			t.Fatalf("PrepareCode(minify=%v): %v", minify, err)
		}
		got, chunks := uploadedCode(t, src, minify)
		if got != string(want) {
			t.Errorf("minify=%v: uploaded code differs from PrepareCode (%d vs %d bytes)", minify, len(got), len(want))
		}
		if minify {
			if len(want) >= len(src) {
				t.Errorf("minified form is not smaller: %d >= %d", len(want), len(src))
			}
			if strings.Contains(string(want), "`") {
				t.Error("template literal was not downgraded")
			}
			if chunks < 2 {
				t.Errorf("want a multi-chunk upload, got %d chunk(s)", chunks)
			}
		} else if got != string(src) {
			t.Error("minify=false must send the source as is")
		}
	}
}
