package ui

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/asnowfix/home-automation/internal/global"
	"github.com/asnowfix/home-automation/internal/myhome/ui/static"
)

func TestStaticAssetsFontAndTokens(t *testing.T) {
	want := map[string]string{
		"/static/InterVariable-4.1.woff2": "font/woff2",
		"/static/Inter-OFL.txt":           "text/plain; charset=utf-8",
		"/static/myhome.css":              "text/css; charset=utf-8",
	}
	for path, ct := range want {
		a, ok := static.Assets[path]
		if !ok {
			t.Fatalf("%s missing from static.Assets", path)
		}
		if a.ContentType != ct {
			t.Errorf("%s content type = %q, want %q", path, a.ContentType, ct)
		}
		if len(a.Content) == 0 {
			t.Errorf("%s has an empty body", path)
		}
	}
	if !bytes.HasPrefix(static.Assets["/static/InterVariable-4.1.woff2"].Content, []byte("wOF2")) {
		t.Error("font is not a WOFF2 file")
	}
	if !bytes.Contains(static.Assets["/static/Inter-OFL.txt"].Content, []byte("SIL Open Font License")) {
		t.Error("licence file does not look like the OFL")
	}
}

func TestMyhomeCSSTokensAndFontFace(t *testing.T) {
	css := string(static.Assets["/static/myhome.css"].Content)
	for _, s := range []string{"--mh-bg: #151617", "--mh-primary: #2491ff", "--mh-tap: 48px", "--mh-rail-width: 80px", "font-weight: 100 900"} {
		if !strings.Contains(css, s) {
			t.Errorf("myhome.css lacks %q", s)
		}
	}
	// every font url in the stylesheet must be a served asset, with no remote host
	if strings.Contains(css, "http://") || strings.Contains(css, "https://") {
		t.Error("myhome.css references a remote URL")
	}
	if !strings.Contains(css, "url(\"/static/InterVariable-4.1.woff2\")") {
		t.Error("@font-face does not point at the embedded font")
	}
}

func TestRenderedPagesCacheBustAssets(t *testing.T) {
	ctx := context.WithValue(context.Background(), global.VersionKey, "9.9.9-test")
	renders := map[string]func(context.Context, *bytes.Buffer) error{
		"index":     func(c context.Context, b *bytes.Buffer) error { return RenderIndex(c, b) },
		"event-log": func(c context.Context, b *bytes.Buffer) error { return RenderEventLog(c, b) },
	}
	for name, render := range renders {
		var buf bytes.Buffer
		if err := render(ctx, &buf); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		html := buf.String()
		for _, a := range []string{"bulma.min.css", "myhome.css", "htmx.min.js", "alpine.min.js"} {
			if !strings.Contains(html, "/static/"+a+"?v=9.9.9-test") {
				t.Errorf("%s: /static/%s lacks ?v=<version>", name, a)
			}
		}
		if !strings.Contains(html, `content="#151617"`) {
			t.Errorf("%s: theme-color not set to the dark background", name)
		}
	}
}

// A versioned URL must still hit the asset handler, which looks up the path only.
func TestStaticHandlerIgnoresVersionQuery(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset, ok := static.Assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", asset.ContentType)
		w.Write(asset.Content)
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/static/myhome.css?v=1.2.3", nil))
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("versioned css request: code %d, %d bytes", rec.Code, rec.Body.Len())
	}
}
