// auth-gate — 统一的 forward-auth 网关（密码登录，跨子域 SSO）。
// 由 Caddy forward_auth 调用：/verify 判断是否已登录；/login /logout 处理登录登出。
// 会话 Cookie 设在父域（如 ryoxu.me），一次登录覆盖所有 *.ryoxu.me 的受保护服务。
package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	host         = env("GATE_HOST", "127.0.0.1")
	port         = env("GATE_PORT", "3001")
	cookieName   = env("GATE_COOKIE_NAME", "__Secure-session")
	cookieDomain = env("GATE_COOKIE_DOMAIN", "") // 设为父域可跨子域 SSO；留空则仅当前主机
	loginPath    = env("GATE_LOGIN_PATH", "/_auth") // 各站点暴露的公共前缀
	sessionTTL   = envInt("GATE_SESSION_TTL", 180*24*60*60)
	title        = env("GATE_TITLE", "Login") // 登录页标题/品牌
	passwordHash string
	secret       []byte
)

const (
	failWindow = 15 * time.Minute
	failLimit  = 8
)

var (
	failMu   sync.Mutex
	failures = map[string][]time.Time{}
)

var b64 = base64.RawURLEncoding

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func mustEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing required env %s\n", k)
		os.Exit(1)
	}
	return v
}

// ---------- 密码 / 会话 ----------

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	dk := make([]byte, 0, blocks*hashLen)
	for b := 1; b <= blocks; b++ {
		prf := hmac.New(sha256.New, password)
		prf.Write(salt)
		var idx [4]byte
		binary.BigEndian.PutUint32(idx[:], uint32(b))
		prf.Write(idx[:])
		u := prf.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for n := 2; n <= iter; n++ {
			prf.Reset()
			prf.Write(u)
			u = prf.Sum(nil)
			for i := range t {
				t[i] ^= u[i]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func verifyPassword(password string) bool {
	parts := strings.SplitN(passwordHash, "$", 4)
	if len(parts) != 4 || parts[0] != "pbkdf2_sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil {
		return false
	}
	salt, err := b64.DecodeString(parts[2])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iter, sha256.Size)
	return subtle.ConstantTimeCompare([]byte(b64.EncodeToString(got)), []byte(parts[3])) == 1
}

func sign(payload string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(payload))
	return b64.EncodeToString([]byte(payload)) + "." + b64.EncodeToString(mac.Sum(nil))
}

func makeToken() string {
	nonce := make([]byte, 18)
	rand.Read(nonce)
	return sign(fmt.Sprintf("%d:%s", time.Now().Unix()+sessionTTL, b64.EncodeToString(nonce)))
}

func validToken(token string) bool {
	dot := strings.LastIndex(token, ".")
	if dot < 0 {
		return false
	}
	payloadBytes, err := b64.DecodeString(token[:dot])
	if err != nil {
		return false
	}
	payload := string(payloadBytes)
	expected := sign(payload)
	expSig := expected[strings.LastIndex(expected, ".")+1:]
	if subtle.ConstantTimeCompare([]byte(token[dot+1:]), []byte(expSig)) != 1 {
		return false
	}
	colon := strings.Index(payload, ":")
	if colon < 0 {
		return false
	}
	exp, err := strconv.ParseInt(payload[:colon], 10, 64)
	return err == nil && exp >= time.Now().Unix()
}

func authed(r *http.Request) bool {
	c, err := r.Cookie(cookieName)
	return err == nil && validToken(c.Value)
}

// ---------- 限流 ----------

func clientKey(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
	}
	return r.RemoteAddr
}

func rateLimited(key string) bool {
	cutoff := time.Now().Add(-failWindow)
	failMu.Lock()
	defer failMu.Unlock()
	return len(trimFailuresLocked(key, cutoff)) >= failLimit
}

func recordFailure(key string) {
	cutoff := time.Now().Add(-failWindow)
	failMu.Lock()
	defer failMu.Unlock()
	failures[key] = append(trimFailuresLocked(key, cutoff), time.Now())
}

func trimFailuresLocked(key string, cutoff time.Time) []time.Time {
	var recent []time.Time
	for _, t := range failures[key] {
		if t.After(cutoff) {
			recent = append(recent, t)
		}
	}
	if len(recent) == 0 {
		delete(failures, key)
	} else {
		failures[key] = recent
	}
	return recent
}

func clearFailures(key string) {
	failMu.Lock()
	delete(failures, key)
	failMu.Unlock()
}

// ---------- 工具 ----------

func safeNext(v string) string {
	if v == "" || !strings.HasPrefix(v, "/") || strings.HasPrefix(v, "//") ||
		strings.ContainsAny(v, "\r\n") {
		return "/"
	}
	return v
}

func htmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&#x27;").Replace(s)
}

func commonHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Frame-Options", "DENY")
}

func setSession(w http.ResponseWriter, value string, maxAge int) {
	c := &http.Cookie{
		Name: cookieName, Value: value, Path: "/", Domain: cookieDomain,
		MaxAge: maxAge, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
	}
	w.Header().Add("Set-Cookie", c.String())
}

const loginTmpl = `<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>__TITLE__</title><style>
:root{color-scheme:dark}*{box-sizing:border-box}
body{margin:0;min-height:100vh;display:grid;place-items:center;font:16px/1.5 system-ui,sans-serif;
background:radial-gradient(1100px 560px at 50% -12%,#1b2030,#0c0d11);color:#e8eaed}
main{width:min(92vw,340px);padding:30px 28px;border:1px solid #2a2f3a;border-radius:18px;
background:#15171d;box-shadow:0 20px 60px #0009}
h1{margin:0 0 6px;font-size:24px;letter-spacing:.2px}
.sub{margin:0 0 22px;color:#9aa0ab;font-size:14px}
.error{margin:0 0 14px;color:#ff7b7b;font-size:13px}
input{width:100%;height:46px;padding:0 14px;border:1px solid #353b47;border-radius:11px;
background:#0e1014;color:#fff;font:inherit;outline:none;transition:border-color .15s,box-shadow .15s}
input:focus{border-color:#3874ff;box-shadow:0 0 0 3px #3874ff33}
button{width:100%;height:46px;margin-top:12px;border:0;border-radius:11px;background:#3874ff;
color:#fff;font:inherit;font-weight:600;cursor:pointer;transition:background .15s}
button:hover{background:#2f63e0}
</style></head><body><main>
<h1>__TITLE__</h1><p class="sub">输入密码继续，设备会保持登录。</p>__ERROR__
<form method="post" action="__ACTION__"><input type="hidden" name="next" value="__NEXT__">
<input name="password" type="password" placeholder="密码" autocomplete="current-password" required autofocus>
<button type="submit">登录</button></form></main></body></html>`

func renderLogin(w http.ResponseWriter, next, errMsg string, statusCode int) {
	errHTML := ""
	if errMsg != "" {
		errHTML = `<p class="error">` + htmlEscape(errMsg) + `</p>`
	}
	page := loginTmpl
	page = strings.ReplaceAll(page, "__TITLE__", htmlEscape(title))
	page = strings.ReplaceAll(page, "__ERROR__", errHTML)
	page = strings.ReplaceAll(page, "__ACTION__", htmlEscape(loginPath)+"/login")
	page = strings.ReplaceAll(page, "__NEXT__", htmlEscape(safeNext(next)))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'")
	commonHeaders(w)
	w.WriteHeader(statusCode)
	w.Write([]byte(page))
}

// ---------- HTTP ----------

func handler(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/health", "/healthz":
		w.WriteHeader(http.StatusNoContent)
	case "/verify":
		if authed(r) {
			commonHeaders(w)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		orig := safeNext(r.Header.Get("X-Original-URI"))
		commonHeaders(w)
		http.Redirect(w, r, loginPath+"/login?next="+url.QueryEscape(orig), http.StatusFound)
	case "/login":
		if r.Method == http.MethodPost {
			loginPost(w, r)
			return
		}
		next := safeNext(r.URL.Query().Get("next"))
		if authed(r) {
			commonHeaders(w)
			http.Redirect(w, r, next, http.StatusSeeOther)
			return
		}
		renderLogin(w, next, "", http.StatusOK)
	case "/logout":
		setSession(w, "", 0)
		commonHeaders(w)
		http.Redirect(w, r, loginPath+"/login", http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}

func loginPost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	password := r.PostFormValue("password")
	next := safeNext(r.PostFormValue("next"))
	key := clientKey(r)
	if rateLimited(key) {
		renderLogin(w, next, "尝试次数过多，请稍后再试。", http.StatusTooManyRequests)
		return
	}
	if !verifyPassword(password) {
		recordFailure(key)
		time.Sleep(350 * time.Millisecond)
		renderLogin(w, next, "密码不正确。", http.StatusUnauthorized)
		return
	}
	clearFailures(key)
	setSession(w, makeToken(), int(sessionTTL))
	commonHeaders(w)
	http.Redirect(w, r, next, http.StatusSeeOther)
}

// ---------- genenv ----------

func genEnv(password string) {
	salt := make([]byte, 16)
	rand.Read(salt)
	const iter = 260000
	dk := pbkdf2SHA256([]byte(password), salt, iter, sha256.Size)
	sec := make([]byte, 36)
	rand.Read(sec)
	fmt.Println("GATE_HOST=127.0.0.1")
	fmt.Println("GATE_PORT=3001")
	fmt.Println("GATE_COOKIE_NAME=__Secure-session")
	fmt.Println("# 设为父域可实现跨子域 SSO（如 example.com）；留空则仅当前主机")
	fmt.Println("GATE_COOKIE_DOMAIN=")
	fmt.Println("GATE_LOGIN_PATH=/_auth")
	fmt.Println("GATE_SESSION_TTL=15552000")
	fmt.Println("GATE_TITLE=Login")
	fmt.Printf("GATE_PASSWORD_HASH=pbkdf2_sha256$%d$%s$%s\n", iter, b64.EncodeToString(salt), b64.EncodeToString(dk))
	fmt.Println("GATE_SECRET=" + b64.EncodeToString(sec))
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "genenv" {
		pw := ""
		if len(os.Args) > 2 {
			pw = os.Args[2]
		} else {
			fmt.Fprint(os.Stderr, "password: ")
			fmt.Scanln(&pw)
		}
		genEnv(pw)
		return
	}
	passwordHash = mustEnv("GATE_PASSWORD_HASH")
	secret = []byte(mustEnv("GATE_SECRET"))
	addr := host + ":" + port
	fmt.Printf("auth-gate listening on %s, cookie domain %s\n", addr, cookieDomain)
	if err := http.ListenAndServe(addr, http.HandlerFunc(handler)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
