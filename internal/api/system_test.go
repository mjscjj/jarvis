package api

import (
	"context"
	"net"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/test/mock"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeShutdowner struct {
	called bool
}

func (f *fakeShutdowner) Shutdown(context.Context) error {
	f.called = true
	return nil
}

type systemRemoteConn struct {
	*mock.Conn
	remote net.Addr
}

func (c *systemRemoteConn) RemoteAddr() net.Addr { return c.remote }

func TestShutdownSystemUsesEffectiveClientAddress(t *testing.T) {
	tests := []struct {
		name      string
		peer      string
		forwarded string
		want      int
	}{
		{name: "local direct", peer: "127.0.0.1", want: consts.StatusAccepted},
		{name: "remote through local gateway", peer: "127.0.0.1", forwarded: "10.0.0.8", want: consts.StatusForbidden},
		{name: "remote direct", peer: "10.0.0.8", want: consts.StatusForbidden},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &fakeShutdowner{}
			request := app.NewContext(0)
			request.SetConn(&systemRemoteConn{
				Conn:   mock.NewConn(""),
				remote: &net.TCPAddr{IP: net.ParseIP(test.peer), Port: 18801},
			})
			if test.forwarded != "" {
				request.Request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			ShutdownSystem(service)(context.Background(), request)
			if request.Response.StatusCode() != test.want {
				t.Fatalf("status = %d, want %d", request.Response.StatusCode(), test.want)
			}
			if service.called != (test.want == consts.StatusAccepted) {
				t.Fatalf("Shutdown called = %t", service.called)
			}
		})
	}
}
