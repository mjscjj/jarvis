// Package uilink owns links from notifications back to the running Jarvis UI.
package uilink

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type Link struct {
	URL   string
	Label string
}

type Resolver struct {
	host           string
	port           string
	resolveLANIPv4 func() (net.IP, error)
}

// New consumes the effective listen address, including the desktop -addr
// override. A specific bind address must never be replaced with another host.
func New(address string) (*Resolver, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil {
		return nil, fmt.Errorf("UI link listen address: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 {
		return nil, fmt.Errorf("UI link requires a fixed valid port: %q", port)
	}
	return &Resolver{host: host, port: port, resolveLANIPv4: currentLANIPv4}, nil
}

func (r *Resolver) Task(id uint64) (Link, error) {
	if id == 0 {
		return Link{}, fmt.Errorf("UI task link requires a positive task ID")
	}
	host := r.host
	ip := net.ParseIP(host)
	if host == "" || (ip != nil && ip.IsUnspecified()) {
		lan, err := r.resolveLANIPv4()
		if err != nil {
			return Link{}, fmt.Errorf("resolve UI link LAN address: %w", err)
		}
		if lan.To4() == nil || !lan.IsPrivate() || lan.IsLoopback() || lan.IsUnspecified() {
			return Link{}, fmt.Errorf("UI link LAN address %q is not a private IPv4", lan)
		}
		host = lan.String()
	}
	label := "查看详情"
	if strings.EqualFold(host, "localhost") || (net.ParseIP(host) != nil && net.ParseIP(host).IsLoopback()) {
		label = "查看详情（Jarvis 所在电脑）"
	}
	return Link{URL: fmt.Sprintf("http://%s/#/work/task/%d", net.JoinHostPort(host, r.port), id), Label: label}, nil
}

func currentLANIPv4() (net.IP, error) {
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
