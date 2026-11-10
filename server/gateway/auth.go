package gateway

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	sessionCookie = "sw_session"
	// csrfHeader must accompany every state-changing request authenticated by
	// cookie. Browsers cannot add custom headers to cross-site form posts, and
	// cross-origin fetches with it trigger a CORS preflight we do not grant.
	csrfHeader     = "X-Drone-Feed"
	loginWindow    = 15 * time.Minute
	loginMaxFailed = 10
)

// A bcrypt hash of a random string, compared against when the username is
// unknown so response timing does not reveal which usernames exist.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), 12)

// bcryptCost for new hashes (tests lower it to stay fast).
var bcryptCost = 12

// HashPassword returns a bcrypt hash for the config file.
func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcryptCost)
	return string(h), err
}

type authState struct {
	secret  []byte
	ttl     time.Duration
	users   map[string]User
	limiter *loginLimiter
}

func newAuthState(cfg *Config) (*authState, bool) {
	a := &authState{ttl: cfg.Auth.SessionTTL, users: map[string]User{}, limiter: &loginLimiter{fails: map[string][]time.Time{}}}
	generated := false
	if cfg.Auth.SessionSecret != "" {
		a.secret = []byte(cfg.Auth.SessionSecret)
	} else {
		a.secret = make([]byte, 32)
		_, _ = rand.Read(a.secret)
		generated = true
	}
	for _, u := range cfg.Auth.Users {
		a.users[u.Username] = u
	}
	return a, generated
}

type sessionClaims struct {
	User string `json:"u"`
	Exp  int64  `json:"e"`
}

func (a *authState) sign(payload []byte) string {
	mac := hmac.New(sha256.New, a.secret)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *authState) issue(user string) (string, time.Time) {
	exp := time.Now().Add(a.ttl)
	payload, _ := json.Marshal(sessionClaims{User: user, Exp: exp.Unix()})
	return base64.RawURLEncoding.EncodeToString(payload) + "." + a.sign(payload), exp
}

// verify returns the logged-in user. The role always comes from the current
// config, so removing a user or changing a role takes effect immediately.
func (a *authState) verify(token string) (User, bool) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok {
		return User{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil || subtle.ConstantTimeCompare([]byte(a.sign(payload)), []byte(sig)) != 1 {
		return User{}, false
	}
	var c sessionClaims
	if json.Unmarshal(payload, &c) != nil || time.Now().Unix() > c.Exp {
		return User{}, false
	}
	u, ok := a.users[c.User]
	return u, ok
}

// loginLimiter blocks an address after too many failed logins.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func (l *loginLimiter) recent(ip string) []time.Time {
	cut := time.Now().Add(-loginWindow)
	var keep []time.Time
	for _, t := range l.fails[ip] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	if keep == nil {
		delete(l.fails, ip)
	} else {
		l.fails[ip] = keep
	}
	return keep
}

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip)) >= loginMaxFailed
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.fails[ip] = append(l.recent(ip), time.Now())
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (g *Gateway) setSessionCookie(w http.ResponseWriter, r *http.Request, value string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: value, Path: "/", Expires: exp, MaxAge: int(time.Until(exp).Seconds()),
		HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode,
	})
}

// authOpen is true when neither users nor tokens are configured (development).
func (g *Gateway) authOpen() bool { return len(g.cfg.Tokens) == 0 && len(g.cfg.Auth.Users) == 0 }

func (g *Gateway) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	var b struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &b) {
		return
	}
	// Limit per address *and* username: behind a reverse proxy every client
	// shares one address, and one person's typos must not lock out everyone.
	key := ip + "|" + strings.ToLower(strings.TrimSpace(b.Username))
	if g.auth.limiter.blocked(key) {
		writeErr(w, http.StatusTooManyRequests, "too many failed logins; try again later")
		return
	}
	u, known := g.auth.users[strings.TrimSpace(b.Username)]
	hash := []byte(u.PasswordHash)
	if !known {
		hash = dummyHash
	}
	if bcrypt.CompareHashAndPassword(hash, []byte(b.Password)) != nil || !known {
		g.auth.limiter.fail(key)
		g.audit.Warn("login failed", "username", b.Username, "ip", ip)
		writeErr(w, http.StatusUnauthorized, "wrong username or password")
		return
	}
	g.auth.limiter.reset(key)
	token, exp := g.auth.issue(u.Username)
	g.setSessionCookie(w, r, token, exp)
	g.audit.Info("login", "actor", u.Username, "ip", ip)
	writeJSON(w, 200, map[string]any{"ok": true, "username": u.Username, "role": u.Role})
}

func (g *Gateway) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: isHTTPS(r), SameSite: http.SameSiteStrictMode})
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (g *Gateway) handleMe(w http.ResponseWriter, r *http.Request) {
	a, ok := g.authenticate(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "not logged in", "auth_enabled": true})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "username": a.name, "role": a.role, "auth_enabled": !g.authOpen()})
}

// csrfOK enforces the custom header on cookie-authenticated writes.
func csrfOK(r *http.Request, a actor) bool {
	if !a.viaCookie {
		return true
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return r.Header.Get(csrfHeader) != ""
}
