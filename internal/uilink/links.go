// Package uilink owns links from notifications back to the running Jarvis UI.
package uilink

import (
	"fmt"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"
)

type Link struct {
	URL   string
	Label string
}

type Resolver struct {
	publicURL   *url.URL
	host        string
	port        string
	localOnly   bool
	resolveIPv4 func() (net.IP, error)
}

// New fixes macOS links to loopback. Linux defaults to the LAN address and
// allows public_url to override it for a reverse proxy or domain. The actual
// listen port (including the desktop -addr override) is never hardcoded.
func New(address, publicURL string) (*Resolver, error) {
	return newForPlatform(address, publicURL, runtime.GOOS)
}

func newForPlatform(address, publicURL, platform string) (*Resolver, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("UI link listen address: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, fmt.Errorf("UI link requires a fixed valid port: %q", port)
	}
	r := &Resolver{host: host, port: port, localOnly: platform == "darwin", resolveIPv4: currentIPv4}
	if raw := strings.TrimSpace(publicURL); raw != "" {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("server.public_url: %w", err)
		}
		if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
			return nil, fmt.Errorf("server.public_url must be an absolute http(s) base URL without credentials, query or fragment")
		}
		if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsUnspecified() {
			return nil, fmt.Errorf("server.public_url must use a browser-reachable host, not a wildcard address")
		}
		if port := u.Port(); port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 1 || n > 65535 {
				return nil, fmt.Errorf("server.public_url requires a valid port: %q", port)
			}
		}
		r.publicURL = u
	}
	return r, nil
}

func (r *Resolver) Task(id uint64) (Link, error) {
	if id == 0 {
		return Link{}, fmt.Errorf("UI task link requires a positive task ID")
	}
	if r.localOnly {
		host := "127.0.0.1"
		if r.host == "::1" || r.host == "::" {
			host = "::1"
		}
		return Link{URL: fmt.Sprintf("http://%s/#/work/task/%d", net.JoinHostPort(host, r.port), id), Label: detailLabel(host)}, nil
	}
	if r.publicURL != nil {
		u := *r.publicURL
		if u.Path == "" {
			u.Path = "/"
		}
		u.Fragment = fmt.Sprintf("/work/task/%d", id)
		return Link{URL: u.String(), Label: detailLabel(u.Hostname())}, nil
	}
	host := r.host
	ip := net.ParseIP(host)
	if host == "" || strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || (ip != nil && (ip.IsUnspecified() || ip.IsLoopback())) {
		address, err := r.resolveIPv4()
		if err != nil {
			return Link{}, fmt.Errorf("resolve UI link address: %w", err)
		}
		if address.To4() == nil || !address.IsGlobalUnicast() || address.IsLoopback() {
			return Link{}, fmt.Errorf("UI link address %q is not a unicast IPv4", address)
		}
		host = address.String()
	}
	return Link{URL: fmt.Sprintf("http://%s/#/work/task/%d", net.JoinHostPort(host, r.port), id), Label: detailLabel(host)}, nil
}

func detailLabel(host string) string {
	if strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		return "查看详情（本机访问）"
	}
	return "查看详情"
}

func currentIPv4() (net.IP, error) {
	connection, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP("1.1.1.1"), Port: 53})
	if err != nil {
		return nil, err
	}
	defer connection.Close()
	address, ok := connection.LocalAddr().(*net.UDPAddr)
	if !ok || address.IP == nil {
		return nil, fmt.Errorf("default route returned no local IPv4")
	}
	return address.IP, nil
}
