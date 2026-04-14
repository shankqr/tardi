// Command dashboard-shim is the auth gateway in front of the Hermes web
// dashboard. The Hermes dashboard (port 9119) ships with no authentication
// and is meant for loopback only — this binary sits in front of it, validates
// a token delivered via URL hash fragment, and reverse-proxies authenticated
// requests through to the dashboard.
//
// Flow:
//  1. Tardi opens https://<domain>/#token=<API_SERVER_KEY> in a new tab.
//  2. Caddy routes the root request to this shim on port 9118.
//  3. The shim's HTML landing page reads location.hash via JS, POSTs the
//     token to /__tardi/auth, and the shim sets an HttpOnly session cookie.
//  4. JS reloads the page; the cookie is now present, the shim proxies to
//     localhost:9119, and the user lands in the Hermes dashboard.
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
)

const cookieName = "tardi_dash"

var landingHTML = []byte(`<!doctype html>
<html><head><meta charset="utf-8"><title>Tardi Dashboard</title>
<style>body{font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}.box{text-align:center}</style>
</head><body><div class="box"><p id="msg">Authenticating…</p></div>
<script>
(function(){
  var msg=document.getElementById('msg');
  var hash=location.hash||'';
  var m=hash.match(/[#&]token=([^&]+)/);
  if(!m){msg.textContent='Missing token. Open this dashboard from your Tardi instance page.';return;}
  var token=decodeURIComponent(m[1]);
  var body=new URLSearchParams();body.set('token',token);
  fetch('/__tardi/auth',{method:'POST',body:body,credentials:'same-origin'})
    .then(function(r){if(!r.ok)throw new Error('auth failed: '+r.status);
      var next=new URLSearchParams(location.search).get('next')||'/';
      location.replace(next);})
    .catch(function(e){msg.textContent=String(e);});
})();
</script></body></html>`)

func main() {
	listen := envOr("LISTEN", "127.0.0.1:9118")
	backendURL := envOr("DASHBOARD_BACKEND", "http://127.0.0.1:9119")
	apiKey := os.Getenv("API_SERVER_KEY")
	if apiKey == "" {
		log.Fatal("dashboard-shim: API_SERVER_KEY is required")
	}

	keyHash := sha256.Sum256([]byte(apiKey))
	cookieValue := hex.EncodeToString(keyHash[:])

	target, err := url.Parse(backendURL)
	if err != nil {
		log.Fatalf("dashboard-shim: invalid DASHBOARD_BACKEND %q: %v", backendURL, err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	origDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		origDirector(req)
		req.Host = target.Host
		req.Header.Del("Authorization")
		req.Header.Set("X-Forwarded-Proto", "https")
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/__tardi/auth", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(landingHTML)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			submitted := r.PostFormValue("token")
			if subtle.ConstantTimeCompare([]byte(submitted), []byte(apiKey)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			http.SetCookie(w, &http.Cookie{
				Name:     cookieName,
				Value:    cookieValue,
				Path:     "/",
				HttpOnly: true,
				Secure:   true,
				SameSite: http.SameSiteStrictMode,
				MaxAge:   60 * 60 * 24 * 7,
			})
			w.WriteHeader(http.StatusNoContent)
		default:
			w.Header().Set("Allow", "GET, POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(cookieValue)) == 1 {
			proxy.ServeHTTP(w, r)
			return
		}
		next := r.URL.RequestURI()
		redirect := "/__tardi/auth"
		if next != "/" && !strings.HasPrefix(next, "/__tardi/") {
			redirect += "?next=" + url.QueryEscape(next)
		}
		http.Redirect(w, r, redirect, http.StatusFound)
	})

	log.Printf("dashboard-shim: listening on %s, proxying to %s", listen, backendURL)
	srv := &http.Server{Addr: listen, Handler: mux}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("dashboard-shim: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
