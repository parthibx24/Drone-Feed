package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func testGateway(t *testing.T, mutate func(*Config)) (*Gateway, *httptest.Server) {
	t.Helper()
	bcryptCost = bcrypt.MinCost
	opHash, _ := HashPassword("operator-pass")
	viewHash, _ := HashPassword("viewer-pass")
	cfg := &Config{
		RecordingsDir: t.TempDir(),
		Auth: AuthConfig{SessionSecret: "test-secret", Users: []User{
			{Username: "pilot", PasswordHash: opHash, Role: "operator"},
			{Username: "watcher", PasswordHash: viewHash, Role: "viewer"},
		}},
		Drones: []DroneConfig{{ID: "kotowali", MAVLink: "udp:127.0.0.1:9"}},
	}
	if mutate != nil {
		mutate(cfg)
	}
	if err := cfg.Normalize(); err != nil {
		t.Fatal(err)
	}
	g, err := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)), "test")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(g.Handler())
	t.Cleanup(srv.Close)
	return g, srv
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
}

func newClient(t *testing.T, base string) *client {
	jar, _ := cookiejar.New(nil)
	return &client{t: t, base: base, http: &http.Client{Jar: jar}}
}

func (c *client) do(method, path, body string, headers map[string]string) (int, map[string]any, *http.Response) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.base+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out, resp
}

var csrf = map[string]string{csrfHeader: "1"}

func TestLoginSessionAndRoles(t *testing.T) {
	_, srv := testGateway(t, nil)
	c := newClient(t, srv.URL)

	if code, _, _ := c.do("GET", "/api/v1/drones", "", nil); code != 401 {
		t.Fatalf("anonymous list: %d", code)
	}
	if code, body, _ := c.do("GET", "/api/auth/me", "", nil); code != 401 || body["auth_enabled"] != true {
		t.Fatalf("anonymous me: %d %v", code, body)
	}
	if code, _, _ := c.do("POST", "/api/auth/login", `{"username":"pilot","password":"nope"}`, nil); code != 401 {
		t.Fatalf("bad password: %d", code)
	}
	if code, _, _ := c.do("POST", "/api/auth/login", `{"username":"ghost","password":"nope"}`, nil); code != 401 {
		t.Fatalf("unknown user: %d", code)
	}

	code, body, resp := c.do("POST", "/api/auth/login", `{"username":"pilot","password":"operator-pass"}`, nil)
	if code != 200 || body["role"] != "operator" {
		t.Fatalf("login: %d %v", code, body)
	}
	var sc *http.Cookie
	for _, ck := range resp.Cookies() {
		if ck.Name == sessionCookie {
			sc = ck
		}
	}
	if sc == nil || !sc.HttpOnly || sc.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie flags: %+v", sc)
	}

	if code, body, _ := c.do("GET", "/api/auth/me", "", nil); code != 200 || body["username"] != "pilot" {
		t.Fatalf("me: %d %v", code, body)
	}
	if code, _, _ := c.do("GET", "/api/v1/drones", "", nil); code != 200 {
		t.Fatalf("list as pilot: %d", code)
	}
	// Writes with a cookie need the CSRF header...
	if code, body, _ := c.do("POST", "/api/v1/drones/kotowali/arm", "", nil); code != 403 || !strings.Contains(body["error"].(string), csrfHeader) {
		t.Fatalf("write without csrf header: %d %v", code, body)
	}
	// ...and with it the request reaches the drone layer (offline here → 503).
	if code, _, _ := c.do("POST", "/api/v1/drones/kotowali/arm", "", csrf); code != 503 {
		t.Fatalf("write with csrf header: %d", code)
	}

	if code, _, _ := c.do("POST", "/api/auth/logout", "", csrf); code != 200 {
		t.Fatal("logout")
	}
	if code, _, _ := c.do("GET", "/api/v1/drones", "", nil); code != 401 {
		t.Fatalf("after logout: %d", code)
	}

	v := newClient(t, srv.URL)
	if code, _, _ := v.do("POST", "/api/auth/login", `{"username":"watcher","password":"viewer-pass"}`, nil); code != 200 {
		t.Fatal("viewer login")
	}
	if code, _, _ := v.do("GET", "/api/v1/drones/kotowali", "", nil); code != 200 {
		t.Fatal("viewer read")
	}
	if code, _, _ := v.do("POST", "/api/v1/drones/kotowali/arm", "", csrf); code != 403 {
		t.Fatalf("viewer arm: %d", code)
	}
}

func TestTamperedAndRevokedSessions(t *testing.T) {
	g, srv := testGateway(t, nil)
	tok, _ := g.auth.issue("pilot")
	try := func(value string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/api/auth/me", nil)
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: value})
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if try(tok) != 200 {
		t.Fatal("valid token rejected")
	}
	body, sig, _ := strings.Cut(tok, ".")
	if try(body+"x."+sig) != 401 || try(body+"."+sig+"x") != 401 || try("garbage") != 401 {
		t.Fatal("tampered token accepted")
	}
	delete(g.auth.users, "pilot") // user removed from config
	if try(tok) != 401 {
		t.Fatal("removed user still logged in")
	}
}

func TestLoginRateLimit(t *testing.T) {
	_, srv := testGateway(t, nil)
	c := newClient(t, srv.URL)
	for i := 0; i < loginMaxFailed; i++ {
		c.do("POST", "/api/auth/login", `{"username":"pilot","password":"wrong"}`, nil)
	}
	if code, _, _ := c.do("POST", "/api/auth/login", `{"username":"pilot","password":"operator-pass"}`, nil); code != 429 {
		t.Fatalf("expected 429 after %d failures, got %d", loginMaxFailed, code)
	}
}

func TestTokensStillWork(t *testing.T) {
	_, srv := testGateway(t, func(c *Config) { c.Tokens = []Token{{Name: "script", Token: "tok", Role: "operator"}} })
	c := newClient(t, srv.URL)
	// Bearer tokens are not cookies, so no CSRF header is needed.
	if code, _, _ := c.do("POST", "/api/v1/drones/kotowali/arm", "", map[string]string{"Authorization": "Bearer tok"}); code != 503 {
		t.Fatalf("token write: %d", code)
	}
	if code, _, _ := c.do("GET", "/api/v1/drones", "", map[string]string{"Authorization": "Bearer wrong"}); code != 401 {
		t.Fatalf("wrong token: %d", code)
	}
}

func TestOpenModeWhenNothingConfigured(t *testing.T) {
	_, srv := testGateway(t, func(c *Config) { c.Auth.Users = nil })
	c := newClient(t, srv.URL)
	if code, body, _ := c.do("GET", "/api/auth/me", "", nil); code != 200 || body["auth_enabled"] != false {
		t.Fatalf("open mode me: %d %v", code, body)
	}
}

func TestSPAServing(t *testing.T) {
	web := t.TempDir()
	_ = os.WriteFile(filepath.Join(web, "index.html"), []byte("<html>app</html>"), 0o644)
	_ = os.MkdirAll(filepath.Join(web, "assets"), 0o755)
	_ = os.WriteFile(filepath.Join(web, "assets", "app-123.js"), []byte("console.log(1)"), 0o644)
	_, srv := testGateway(t, func(c *Config) { c.WebDir = web })

	get := func(path string) (int, string, http.Header) {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b), resp.Header
	}
	if code, body, _ := get("/drones/kotowali/mission"); code != 200 || !strings.Contains(body, "app") {
		t.Fatalf("client route: %d %q", code, body)
	}
	if code, body, h := get("/assets/app-123.js"); code != 200 || !strings.Contains(body, "console") || !strings.Contains(h.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %v", code, body, h)
	}
	if code, _, _ := get("/api/v1/nope"); code != 404 && code != 401 {
		t.Fatalf("unknown api path: %d", code)
	}
	if _, _, h := get("/"); h.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
	if code, _, _ := get("/../../etc/passwd"); code != 200 { // falls back to index, never escapes
		t.Fatalf("traversal path: %d", code)
	}
}

func TestCameraDiscovery(t *testing.T) {
	go2rtc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"kotowali_camera":{},"kotowali_thermal":{},"kotowali_rear":{},"other_camera":{}}`))
	}))
	defer go2rtc.Close()
	_, srv := testGateway(t, func(c *Config) {
		c.Auth.Users = nil
		c.Go2RTC = Go2RTCConfig{PublicURL: "https://cams.example", APIURL: go2rtc.URL}
	})
	c := newClient(t, srv.URL)
	code, body, _ := c.do("GET", "/api/v1/drones/kotowali/cameras", "", nil)
	if code != 200 || body["go2rtc_url"] != "https://cams.example" || body["discovered"] != true {
		t.Fatalf("cameras: %d %v", code, body)
	}
	if got := body["streams"].([]any); len(got) != 3 || got[0] != "kotowali_camera" {
		t.Fatalf("streams: %v", got)
	}

	g := &Gateway{cfg: &Config{Go2RTC: Go2RTCConfig{APIURL: "https://cams.example", RTSPURL: "rtsp://cams.example:8554", IngestMode: "mp4"}}}
	if s := g.streamSources([]string{"a b"}); s[0].URL != "https://cams.example/api/stream.mp4?src=a+b" {
		t.Fatalf("mp4 ingest url: %s", s[0].URL)
	}
	g.cfg.Go2RTC.IngestMode = "rtsp"
	if s := g.streamSources([]string{"cam"}); s[0].URL != "rtsp://cams.example:8554/cam" {
		t.Fatalf("rtsp ingest url: %s", s[0].URL)
	}
}
