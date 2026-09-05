package api

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/assert"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestGetChatRuntimeConfigReturnsOnlyPort(t *testing.T) {
	h := server.New()
	h.GET("/api/chat-config", GetChatRuntimeConfig("0.0.0.0:18803"))
	response := ut.PerformRequest(h.Engine, "GET", "/api/chat-config", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Port int `json:"port"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Code != 0 || payload.Data.Port != 18803 {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestAllowedChatOriginAcceptsAnyPortOnSameHost(t *testing.T) {
	assert.True(t, allowedChatOrigin("http://192.168.3.91:18802", "192.168.3.91:18803"))
	assert.True(t, allowedChatOrigin("http://localhost:18802", "localhost:18803"))
	assert.True(t, allowedChatOrigin("http://localhost:5173", "localhost:18803"))
	assert.True(t, allowedChatOrigin("https://emily.bytedance.net", "emily.bytedance.net"))
	assert.True(t, allowedChatOrigin("https://emily.bytedance.net:443", "emily.bytedance.net"))
	assert.False(t, allowedChatOrigin("http://attacker.test:18802", "localhost:18803"))
	assert.False(t, allowedChatOrigin("file:///tmp/index.html", "localhost:18803"))
}
