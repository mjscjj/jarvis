// Package okrchat owns the OKR product's execution boundary, not pipeline ACLs.
package okrchat

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strings"
	"time"

	"jarvis/internal/toolcatalog"
)

// NewAccessHandler exposes two operations on the one container-visible socket:
// fixed OKR HTTP routes and TLS CONNECT to explicitly configured model hosts.
// It never acts as an arbitrary HTTP forward proxy.
func NewAccessHandler(upstream string, modelHosts []string) (http.Handler, error) {
	u, err := url.Parse(upstream)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.User != nil {
		return nil, fmt.Errorf("invalid OKR upstream")
	}
	allowed := map[string]bool{}
	for _, host := range modelHosts {
		name, port, err := net.SplitHostPort(host)
		if err != nil || port != "443" || name == "" || net.ParseIP(name) != nil || strings.ContainsAny(name, "/*@ ") || strings.EqualFold(name, u.Hostname()) {
			return nil, fmt.Errorf("model target must be an exact DNS host:443 distinct from Jarvis")
		}
		allowed[strings.ToLower(host)] = true
	}
	proxy := &httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(u)
		// The host supplies the routing target. Never inherit browser identity,
		// proxy metadata, or an Agent-provided Host override.
		p.Out.Host = u.Host
		p.Out.Header = make(http.Header)
		p.Out.Header.Set("Content-Type", p.In.Header.Get("Content-Type"))
	}, Transport: &http.Transport{Proxy: nil, ResponseHeaderTimeout: 30 * time.Second}}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			if !allowed[strings.ToLower(r.Host)] {
				http.Error(w, "model target denied", 403)
				return
			}
			dialer := &net.Dialer{Timeout: 10 * time.Second}
			// Resolve ourselves and connect to the checked IP, avoiding DNS rebinding
			// to loopback/private services. TLS remains end-to-end in the CLI.
			host, port, _ := net.SplitHostPort(r.Host)
			ips, err := net.DefaultResolver.LookupIPAddr(r.Context(), host)
			if err != nil || len(ips) == 0 {
				http.Error(w, "model DNS failed", 502)
				return
			}
			for _, ip := range ips {
				if !ip.IP.IsGlobalUnicast() || ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() {
					http.Error(w, "model address denied", 403)
					return
				}
			}
			conn, err := dialer.DialContext(r.Context(), "tcp", net.JoinHostPort(ips[0].IP.String(), port))
			if err != nil {
				http.Error(w, "model connection failed", 502)
				return
			}
			defer conn.Close()
			h, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "CONNECT unavailable", 500)
				return
			}
			client, buffered, err := h.Hijack()
			if err != nil {
				return
			}
			defer client.Close()
			_ = conn.SetDeadline(time.Now().Add(time.Hour))
			_ = client.SetDeadline(time.Now().Add(time.Hour))
			if _, err := buffered.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
				return
			}
			if buffered.Flush() != nil {
				return
			}
			done := make(chan struct{})
			go func() { _, _ = io.Copy(conn, buffered); _ = conn.Close(); close(done) }()
			_, _ = io.Copy(client, conn)
			_ = client.Close()
			<-done
			return
		}
		if r.URL.IsAbs() || r.URL.RawPath != "" || !toolcatalog.OKRChatAllowed(r.Method, r.URL.Path) {
			http.Error(w, "OKR tool denied", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 13<<20)
		proxy.ServeHTTP(w, r)
	}), nil
}

func ListenAccess(socket string, handler http.Handler) (*http.Server, error) {
	// A stale socket is not silently removed: startup must not replace another
	// process's live gateway. Deployment cleans up through normal shutdown.
	l, err := net.Listen("unix", socket)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(socket, 0600); err != nil {
		l.Close()
		return nil, err
	}
	s := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	go func() { _ = s.Serve(l) }()
	return s, nil
}

func closeAccess(s *http.Server) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.Shutdown(ctx)
}
