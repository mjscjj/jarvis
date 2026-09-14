package api

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDevelopmentPrefixRoutesAndCookies(t *testing.T) {
	for _, prefix := range []string{"/dev/", "/sandbox/emily/"} {
		t.Run(prefix, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "dev-proxy-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			socket := filepath.Join(dir, "web.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			backend := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/login" {
					http.SetCookie(w, &http.Cookie{Name: "jarvis_session", Value: "development", Path: "/", HttpOnly: true})
					w.Header().Set("Location", "/#/biz-okr")
					w.WriteHeader(302)
					return
				}
				cookie, _ := r.Cookie("jarvis_session")
				if cookie == nil || cookie.Value != "development" {
					t.Error("wrong session forwarded")
				}
				if strings.Contains(r.Header.Get("Cookie"), "production") {
					t.Error("production cookie forwarded")
				}
				fmt.Fprint(w, r.URL.RequestURI())
			})}
			go backend.Serve(listener)
			defer backend.Close()
			proxy := newDevelopmentHTTPProxy(socket, prefix)
			login := httptest.NewRecorder()
			proxy.ServeHTTP(login, httptest.NewRequest("GET", "http://example.com"+prefix+"login", nil))
			response := login.Result()
			if response.Header.Get("Location") != prefix+"#/biz-okr" {
				t.Fatal(response.Header)
			}
			cookies := response.Cookies()
			if len(cookies) != 1 || cookies[0].Path != prefix || !cookies[0].HttpOnly {
				t.Fatal(cookies)
			}
			request := httptest.NewRequest("GET", "http://example.com"+prefix+"api/tasks?page=2", nil)
			request.AddCookie(&http.Cookie{Name: "jarvis_session", Value: "production"})
			request.AddCookie(cookies[0])
			recorder := httptest.NewRecorder()
			proxy.ServeHTTP(recorder, request)
			raw, _ := io.ReadAll(recorder.Result().Body)
			if string(raw) != "/api/tasks?page=2" {
				t.Fatalf("wrong upstream path: %s", raw)
			}
		})
	}
}
