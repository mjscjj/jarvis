package api

import (
	"encoding/json"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/test/assert"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func webConfigPublicBaseURL(t *testing.T, configured string) string {
	t.Helper()
	h := server.New()
	h.GET("/api/web-config", GetWebConfig(configured))
	response := ut.PerformRequest(h.Engine, "GET", "/api/web-config", nil).Result()
	assert.DeepEqual(t, 200, response.StatusCode())
	var payload struct {
		Code int `json:"code"`
		Data struct {
			PublicBaseURL string `json:"public_base_url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	assert.DeepEqual(t, 0, payload.Code)
	return payload.Data.PublicBaseURL
}

func TestGetWebConfigReportsConfiguredPublicBaseURL(t *testing.T) {
	assert.DeepEqual(t, "http://emily.example:18802", webConfigPublicBaseURL(t, "http://emily.example:18802"))
}

// An unnamed deployment reports an empty address so the browser keeps building
// links from the one it already used, instead of guessing a hostname.
func TestGetWebConfigReportsEmptyPublicBaseURLWhenUnset(t *testing.T) {
	assert.DeepEqual(t, "", webConfigPublicBaseURL(t, ""))
}
