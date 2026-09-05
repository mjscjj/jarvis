package api

import (
	"net"
	"testing"
)

type testAddress string

func (a testAddress) Network() string { return "tcp" }
func (a testAddress) String() string  { return string(a) }

func TestIsLoopbackAddress(t *testing.T) {
	tests := []struct {
		address net.Addr
		want    bool
	}{
		{testAddress("127.0.0.1:18800"), true},
		{testAddress("[::1]:18800"), true},
		{testAddress("192.168.1.2:18800"), false},
		{testAddress("invalid"), false},
		{nil, false},
	}
	for _, test := range tests {
		if got := isLoopbackAddress(test.address); got != test.want {
			t.Fatalf("isLoopbackAddress(%v) = %v, want %v", test.address, got, test.want)
		}
	}
}
