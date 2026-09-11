package uilink

import (
	"errors"
	"net"
	"testing"
)

func TestSpecificBindAddressDoesNotNeedNetwork(t *testing.T) {
	for _, tc := range []struct {
		address, url string
		local        bool
	}{
		{"192.168.1.20:18800", "http://192.168.1.20:18800/#/work/task/7", false},
		{"203.0.113.20:18800", "http://203.0.113.20:18800/#/work/task/7", false},
		{"jarvis.local:18800", "http://jarvis.local:18800/#/work/task/7", false},
	} {
		t.Run(tc.address, func(t *testing.T) {
			r, err := newForPlatform(tc.address, "", "linux")
			if err != nil {
				t.Fatal(err)
			}
			r.resolveIPv4 = func() (net.IP, error) { t.Fatal("specific address must work offline"); return nil, nil }
			got, err := r.Task(7)
			if err != nil {
				t.Fatal(err)
			}
			if got.URL != tc.url || (got.Label == "查看详情（本机访问）") != tc.local {
				t.Fatalf("link = %+v", got)
			}
		})
	}
}

func TestLinuxDefaultResolvesCurrentAddress(t *testing.T) {
	for _, address := range []string{"0.0.0.0:18800", ":18800", "[::]:18800", "127.0.0.1:18800", "localhost:18800", "[::1]:18800"} {
		r, err := newForPlatform(address, "", "linux")
		if err != nil {
			t.Fatal(err)
		}
		current := "192.168.1.20"
		r.resolveIPv4 = func() (net.IP, error) { return net.ParseIP(current), nil }
		for _, ip := range []string{"192.168.1.20", "10.0.0.2", "203.0.113.20"} {
			current = ip
			link, err := r.Task(7)
			if err != nil || link.URL != "http://"+ip+":18800/#/work/task/7" {
				t.Fatalf("link=%+v err=%v", link, err)
			}
		}
		r.resolveIPv4 = func() (net.IP, error) { return nil, errors.New("no route") }
		if _, err := r.Task(7); err == nil {
			t.Fatal("expected route error")
		}
		r.resolveIPv4 = func() (net.IP, error) { return net.ParseIP("127.0.0.1"), nil }
		if _, err := r.Task(7); err == nil {
			t.Fatal("expected invalid address error")
		}
	}
}

func TestInvalidAddressAndTask(t *testing.T) {
	for _, address := range []string{"", "localhost", "127.0.0.1:0", "127.0.0.1:65536", "127.0.0.1:http"} {
		if _, err := New(address, ""); err == nil {
			t.Fatalf("accepted %q", address)
		}
	}
	r, _ := New("127.0.0.1:18800", "")
	if _, err := r.Task(0); err == nil {
		t.Fatal("accepted zero task")
	}
}

func TestPublicBaseURLOverridesListenAddress(t *testing.T) {
	for _, tc := range []struct{ base, want, label string }{
		{"https://jarvis.example.com", "https://jarvis.example.com/#/work/task/7", "查看详情"},
		{"https://jarvis.example.com:8443/proxy/", "https://jarvis.example.com:8443/proxy/#/work/task/7", "查看详情"},
		{"http://10.0.0.2:28800", "http://10.0.0.2:28800/#/work/task/7", "查看详情"},
		{"https://[2001:db8::1]:8443/", "https://[2001:db8::1]:8443/#/work/task/7", "查看详情"},
		{"http://localhost:28800", "http://localhost:28800/#/work/task/7", "查看详情（本机访问）"},
	} {
		for _, bind := range []string{"127.0.0.1:19900", "0.0.0.0:18800"} {
			t.Run(bind+"/"+tc.base, func(t *testing.T) {
				r, err := newForPlatform(bind, tc.base, "linux")
				if err != nil {
					t.Fatal(err)
				}
				r.resolveIPv4 = func() (net.IP, error) { t.Fatal("explicit URL must not resolve LAN"); return nil, nil }
				got, err := r.Task(7)
				if err != nil || got.URL != tc.want || got.Label != tc.label {
					t.Fatalf("link=%+v err=%v", got, err)
				}
			})
		}
	}
}

func TestMacUsesLocalAddressWithoutPublicBaseURL(t *testing.T) {
	for _, tc := range []struct{ bind, want string }{
		{"127.0.0.1:19900", "http://127.0.0.1:19900/#/work/task/7"},
		{"0.0.0.0:18800", "http://127.0.0.1:18800/#/work/task/7"},
		{"192.168.1.20:18800", "http://127.0.0.1:18800/#/work/task/7"},
		{"[::1]:19900", "http://[::1]:19900/#/work/task/7"},
	} {
		r, err := newForPlatform(tc.bind, "", "darwin")
		if err != nil {
			t.Fatal(err)
		}
		r.resolveIPv4 = func() (net.IP, error) { t.Fatal("Mac links must work offline"); return nil, nil }
		got, err := r.Task(7)
		if err != nil || got.URL != tc.want || got.Label != "查看详情（本机访问）" {
			t.Fatalf("bind=%s link=%+v err=%v", tc.bind, got, err)
		}
	}
}

func TestMacUsesConfiguredPublicBaseURL(t *testing.T) {
	r, err := newForPlatform("127.0.0.1:19900", "https://jarvis.example.com:8443/proxy/", "darwin")
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.Task(7)
	if err != nil || got.URL != "https://jarvis.example.com:8443/proxy/#/work/task/7" || got.Label != "查看详情" {
		t.Fatalf("link=%+v err=%v", got, err)
	}
}

func TestInvalidPublicBaseURL(t *testing.T) {
	for _, raw := range []string{"jarvis.example.com", "/relative", "ftp://jarvis.example.com", "https://", "http://0.0.0.0:18800", "http://[::]:18800", "https://user:pass@example.com", "https://example.com?x=1", "https://example.com?", "https://example.com/#/work/task/1", "https://example.com/#", "http://example.com:0", "http://example.com:65536", "http://example.com:bad"} {
		if _, err := New("127.0.0.1:18800", raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
}
