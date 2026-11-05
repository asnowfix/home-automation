package ui

import (
	"bytes"
	"context"
	"net/http/httptest"
	"regexp"
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
		"index": func(c context.Context, b *bytes.Buffer) error { return RenderIndex(c, b) },
	}
	for name, render := range renders {
		var buf bytes.Buffer
		if err := render(ctx, &buf); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		html := buf.String()
		for _, a := range []string{"bulma.min.css", "myhome.css", "htmx.min.js", "alpine.min.js", "myhome.js", "manifest.json", "penates.svg"} {
			if a == "manifest.json" && name != "index" {
				continue // the event-log page has no manifest link
			}
			if !strings.Contains(html, "/static/"+a+"?v=9.9.9-test") {
				t.Errorf("%s: /static/%s lacks ?v=<version>", name, a)
			}
		}
		if !strings.Contains(html, `content="#151617"`) {
			t.Errorf("%s: theme-color not set to the dark background", name)
		}
	}
}

// A versioned URL must still hit the real asset handler, which looks up the path only.
func TestServeStatic(t *testing.T) {
	rec := httptest.NewRecorder()
	serveStatic(rec, httptest.NewRequest("GET", "/static/myhome.css?v=1.2.3", nil))
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("versioned css request: code %d, %d bytes", rec.Code, rec.Body.Len())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/css; charset=utf-8" {
		t.Errorf("Content-Type = %q", ct)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control = %q", cc)
	}
	rec = httptest.NewRecorder()
	serveStatic(rec, httptest.NewRequest("GET", "/static/nope.css?v=1", nil))
	if rec.Code != 404 {
		t.Errorf("unknown asset: code %d, want 404", rec.Code)
	}
}

func TestCSSURLsAreServedAssets(t *testing.T) {
	css := string(static.Assets["/static/myhome.css"].Content)
	urls := regexp.MustCompile(`url\("?([^")]+)"?\)`).FindAllStringSubmatch(css, -1)
	if len(urls) == 0 {
		t.Fatal("no url() in myhome.css")
	}
	for _, m := range urls {
		if _, ok := static.Assets[m[1]]; !ok {
			t.Errorf("myhome.css url(%s) is not in static.Assets", m[1])
		}
	}
}

func TestAllTokensAndManifestColours(t *testing.T) {
	css := string(static.Assets["/static/myhome.css"].Content)
	for _, tok := range []string{
		"--mh-bg: #151617", "--mh-surface-1: #1b1d21", "--mh-surface-2: #242527", "--mh-surface-3: #303235",
		"--mh-surface-4: #5b5f66", "--mh-card: rgba(52, 55, 58, 0.3)", "--mh-primary: #2491ff",
		"--mh-primary-tint-1: #182f45", "--mh-primary-tint-2: #1a3b5d", "--mh-secondary: #bbdcf8",
		"--mh-text: #eaeaea", "--mh-text-muted: #a1a2a2", "--mh-radius-pill: 200px", "--mh-radius-card: 24px",
		`--mh-font: "Inter", system-ui, sans-serif`, "--mh-font-size: 16px", "--mh-font-weight: 400",
		"--mh-tap: 48px", "--mh-rail-width: 80px",
	} {
		if !strings.Contains(css, tok) {
			t.Errorf("myhome.css lacks token %q", tok)
		}
	}
	m := string(static.Assets["/static/manifest.json"].Content)
	if !strings.Contains(m, `"theme_color": "#151617"`) || !strings.Contains(m, `"background_color": "#151617"`) {
		t.Error("manifest.json colours are not the dark background")
	}
}

func staticAsset(t *testing.T, path string) []byte {
	t.Helper()
	a, ok := static.Assets[path]
	if !ok {
		t.Fatalf("%s not in static.Assets", path)
	}
	return a.Content
}
