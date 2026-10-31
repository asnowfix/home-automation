package cloud

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testBLID     = "0123456789ABCDEF0123456789ABCDEF"
	testEmail    = "owner@example.com"
	testPassword = "account-password-must-not-leak"
	testSig      = "gigya-signature-must-not-leak"
	testSecret   = "aws-secret-must-not-leak"
	testToken    = "aws-session-token-must-not-leak"
	testKeyID    = "ASIAEXAMPLEKEYID"
)

// secrets that must never appear in an error, a result, or a log.
var secrets = []string{testPassword, testSig, testSecret, testToken}

const twoMaps = `[
 {"pmap_id":"m1","user_pmapv_id":"251027T114719","active_pmapv_details":{
   "map_header":{"name":"Rez-de-chaussée"},
   "regions":[{"id":"1","name":"Salle à manger","region_type":"dining_room"},
              {"id":"21","name":"Cuisine","region_type":"kitchen"},
              {"id":8,"name":"Chambre","region_type":"bedroom"}]}},
 {"pmap_id":"m2","user_pmapv_id":"231016T113248","active_pmapv_details":{"map_header":{"name":"Étage"},"regions":[]}}
]`

// fakeCloud is the whole chain on one server. Each step can be overridden.
type fakeCloud struct {
	t   *testing.T
	srv *httptest.Server

	mu     sync.Mutex
	calls  map[string]int
	gigya  url.Values
	login  map[string]string
	authz  string
	token  string
	pmapsQ string

	discover func(w http.ResponseWriter, base string)
	gigyaFn  func(w http.ResponseWriter)
	loginFn  func(w http.ResponseWriter)
	pmapsFn  func(w http.ResponseWriter)
}

func newFakeCloud(t *testing.T) *fakeCloud {
	f := &fakeCloud{t: t, calls: map[string]int{}}
	f.discover = func(w http.ResponseWriter, base string) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"gigya": map[string]string{"api_key": "gigya-api-key"}, "httpBase": base, "httpBaseAuth": base + "/auth",
			"awsRegion": "us-east-1", "mqtt": "x", "irbtTopics": "y",
		})
	}
	f.gigyaFn = func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"errorCode":0,"UID":"uid-1","UIDSignature":"`+testSig+`","signatureTimestamp":"1761730000"}`)
	}
	f.loginFn = func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `{"credentials":{"AccessKeyId":"`+testKeyID+`","SecretKey":"`+testSecret+`","SessionToken":"`+testToken+`"}}`)
	}
	f.pmapsFn = func(w http.ResponseWriter) { _, _ = io.WriteString(w, twoMaps) }

	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls[r.URL.Path]++
		f.mu.Unlock()
		switch {
		case r.URL.Path == "/v1/app/discover":
			if r.URL.Query().Get("country_code") != "FR" {
				t.Errorf("country_code = %q", r.URL.Query().Get("country_code"))
			}
			f.discover(w, f.srv.URL)
		case r.URL.Path == "/accounts.login":
			_ = r.ParseForm()
			f.mu.Lock()
			f.gigya = r.PostForm
			f.mu.Unlock()
			f.gigyaFn(w)
		case r.URL.Path == "/v1/login/account":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("login body: %v", err)
			}
			f.mu.Lock()
			f.login = body
			f.mu.Unlock()
			f.loginFn(w)
		case r.URL.Path == "/auth/v1/"+testBLID+"/pmaps":
			f.mu.Lock()
			f.authz = r.Header.Get("Authorization")
			f.token = r.Header.Get("X-Amz-Security-Token")
			f.pmapsQ = r.URL.RawQuery
			f.mu.Unlock()
			f.pmapsFn(w)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeCloud) client() *Client {
	c := New()
	c.HTTP = f.srv.Client()
	c.HTTP.Timeout = 2 * time.Second
	c.Discovery = f.srv.URL
	c.Gigya = f.srv.URL
	return c
}

func (f *fakeCloud) count(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[path]
}

func noSecrets(t *testing.T, what, s string) {
	t.Helper()
	for _, sec := range secrets {
		if strings.Contains(s, sec) {
			t.Fatalf("%s leaks a secret: %s", what, s)
		}
	}
}

func TestMapsHappyPath(t *testing.T) {
	f := newFakeCloud(t)
	maps, err := f.client().Maps(context.Background(), testEmail, testPassword, testBLID)
	if err != nil {
		t.Fatal(err)
	}
	if len(maps) != 2 {
		t.Fatalf("maps = %+v", maps)
	}
	m := maps[0]
	if m.ID != "m1" || m.Version != "251027T114719" || m.Name != "Rez-de-chaussée" || len(m.Regions) != 3 {
		t.Fatalf("map 1 = %+v", m)
	}
	if m.Regions[1] != (Region{ID: "21", Name: "Cuisine", Type: "kitchen"}) {
		t.Fatalf("region = %+v", m.Regions[1])
	}
	if m.Regions[2].ID != "8" {
		t.Fatalf("numeric id not accepted: %+v", m.Regions[2])
	}
	if maps[1].ID != "m2" || len(maps[1].Regions) != 0 || maps[1].Regions == nil {
		t.Fatalf("second map (no rooms) = %+v", maps[1])
	}

	// What each step sent.
	g := f.gigya
	for k, want := range map[string]string{"ApiKey": "gigya-api-key", "loginID": testEmail, "password": testPassword,
		"format": "json", "sessionExpiration": "-2", "targetEnv": "mobile"} {
		if got := strings.Join(g[k], ","); got != want {
			t.Errorf("gigya %s = %q, want %q", k, got, want)
		}
	}
	for k, want := range map[string]string{"uid": "uid-1", "signature": testSig, "timestamp": "1761730000",
		"assume_robot_ownership": "0", "app_id": DefaultAppID} {
		if f.login[k] != want {
			t.Errorf("login %s = %q, want %q", k, f.login[k], want)
		}
	}
	if !strings.HasPrefix(f.authz, "AWS4-HMAC-SHA256 Credential="+testKeyID+"/") ||
		!strings.Contains(f.authz, "/us-east-1/execute-api/aws4_request") ||
		!strings.Contains(f.authz, "SignedHeaders=host;x-amz-date;x-amz-security-token") {
		t.Errorf("Authorization = %q", f.authz)
	}
	if f.token != testToken {
		t.Error("session token header not sent")
	}
	if f.pmapsQ != "activeDetails=1" {
		t.Errorf("pmaps query = %q", f.pmapsQ)
	}
	b, _ := json.Marshal(maps)
	noSecrets(t, "result", string(b))
}

func TestMapsFailures(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(f *fakeCloud)
		step   Step
		status int
		code   int
		hint   string
	}{
		{"discover 503", func(f *fakeCloud) {
			f.discover = func(w http.ResponseWriter, _ string) { w.WriteHeader(http.StatusServiceUnavailable) }
		}, StepDiscover, 503, 0, ""},
		{"discover incomplete", func(f *fakeCloud) {
			f.discover = func(w http.ResponseWriter, _ string) { _, _ = io.WriteString(w, `{"gigya":{}}`) }
		}, StepDiscover, 200, 0, "lacks"},
		{"gigya bad password", func(f *fakeCloud) {
			f.gigyaFn = func(w http.ResponseWriter) {
				_, _ = io.WriteString(w, `{"errorCode":403042,"errorMessage":"Invalid LoginID","statusCode":403}`)
			}
		}, StepGigya, 200, 403042, "invalid email or password"},
		{"gigya two-factor", func(f *fakeCloud) {
			f.gigyaFn = func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"errorCode":403101}`) }
		}, StepGigya, 200, 403101, "only plain email+password"},
		{"gigya no signature", func(f *fakeCloud) {
			f.gigyaFn = func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"errorCode":0}`) }
		}, StepGigya, 200, 0, "only plain email+password"},
		{"login 403", func(f *fakeCloud) {
			f.loginFn = func(w http.ResponseWriter) { w.WriteHeader(http.StatusForbidden) }
		}, StepLogin, 403, 0, ""},
		{"pmaps 403", func(f *fakeCloud) {
			f.pmapsFn = func(w http.ResponseWriter) { w.WriteHeader(http.StatusForbidden) }
		}, StepMaps, 403, 0, ""},
		{"pmaps 500", func(f *fakeCloud) {
			f.pmapsFn = func(w http.ResponseWriter) { w.WriteHeader(http.StatusInternalServerError) }
		}, StepMaps, 500, 0, ""},
		{"pmaps not a list", func(f *fakeCloud) {
			f.pmapsFn = func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"message":"nope"}`) }
		}, StepMaps, 200, 0, "unexpected reply"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeCloud(t)
			tc.setup(f)
			_, err := f.client().Maps(context.Background(), testEmail, testPassword, testBLID)
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("err = %v, want *cloud.Error", err)
			}
			if ce.Step != tc.step || ce.HTTPStatus != tc.status || ce.GigyaCode != tc.code {
				t.Fatalf("err = %+v", ce)
			}
			if tc.hint != "" && !strings.Contains(err.Error(), tc.hint) {
				t.Fatalf("err %q lacks %q", err, tc.hint)
			}
			if !strings.Contains(err.Error(), string(tc.step)) {
				t.Fatalf("err %q does not name the step", err)
			}
			noSecrets(t, "error", err.Error())
			// One login attempt per call, never a retry.
			if n := f.count("/accounts.login"); n > 1 {
				t.Fatalf("Gigya login called %d times", n)
			}
			if n := f.count("/v1/login/account"); n > 1 {
				t.Fatalf("iRobot login called %d times", n)
			}
		})
	}
}

func TestMapsEmptyRegions(t *testing.T) {
	f := newFakeCloud(t)
	f.pmapsFn = func(w http.ResponseWriter) {
		_, _ = io.WriteString(w, `[{"pmap_id":"m1","user_pmapv_id":"v1","active_pmapv_details":{"map_header":{"name":"A"}}}]`)
	}
	maps, err := f.client().Maps(context.Background(), testEmail, testPassword, testBLID)
	if err != nil || len(maps) != 1 || len(maps[0].Regions) != 0 {
		t.Fatalf("maps = %+v, %v", maps, err)
	}
}

func TestMapsTimeout(t *testing.T) {
	f := newFakeCloud(t)
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	f.discover = func(w http.ResponseWriter, _ string) { <-block }
	c := f.client()
	c.HTTP.Timeout = 100 * time.Millisecond
	start := time.Now()
	_, err := c.Maps(context.Background(), testEmail, testPassword, testBLID)
	var ce *Error
	if !errors.As(err, &ce) || ce.Step != StepDiscover {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("timeout not applied")
	}
}

func TestMapsRequiresCredentials(t *testing.T) {
	c := New()
	c.Discovery, c.Gigya = "http://192.0.2.1:1", "http://192.0.2.1:1" // never reached
	if _, err := c.Maps(context.Background(), "", "", testBLID); err == nil {
		t.Fatal("no error without credentials")
	}
	if _, err := c.Maps(context.Background(), testEmail, testPassword, ""); err == nil {
		t.Fatal("no error without a BLID")
	}
}
