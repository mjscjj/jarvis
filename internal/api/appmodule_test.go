package api

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/appmodule"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type fakeAppModuleService struct {
	items []appmodule.View
	input appmodule.Input
}

func (f *fakeAppModuleService) List(context.Context) ([]appmodule.View, error) {
	return f.items, nil
}

func (f *fakeAppModuleService) Update(_ context.Context, key string, input appmodule.Input) (*appmodule.View, error) {
	f.input = input
	return &appmodule.View{Key: key, Name: "OKR", IsEnabled: input.IsEnabled != nil && *input.IsEnabled}, nil
}

func TestListAppModules(t *testing.T) {
	service := &fakeAppModuleService{items: []appmodule.View{{Key: "okr", Name: "OKR", IsEnabled: true}}}
	h := server.New()
	h.GET("/api/app-modules", ListAppModules(service))
	response := ut.PerformRequest(h.Engine, "GET", "/api/app-modules", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data struct {
			Items []appmodule.View `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Items) != 1 || payload.Data.Items[0].Key != "okr" || !payload.Data.Items[0].IsEnabled {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestUpdateAppModuleUsesStrictJSON(t *testing.T) {
	service := &fakeAppModuleService{}
	h := server.New()
	h.PUT("/api/app-modules/:module_key", UpdateAppModule(service))
	unknown := []byte(`{"is_enabled":true,"unknown":true}`)
	response := ut.PerformRequest(h.Engine, "PUT", "/api/app-modules/okr", &ut.Body{Body: bytes.NewReader(unknown), Len: len(unknown)}).Result()
	if response.StatusCode() != consts.StatusBadRequest {
		t.Fatalf("unknown field status=%d body=%s", response.StatusCode(), response.Body())
	}
	body := []byte(`{"is_enabled":false}`)
	response = ut.PerformRequest(h.Engine, "PUT", "/api/app-modules/okr", &ut.Body{Body: bytes.NewReader(body), Len: len(body)}).Result()
	if response.StatusCode() != consts.StatusOK || service.input.IsEnabled == nil || *service.input.IsEnabled {
		t.Fatalf("update status=%d input=%+v body=%s", response.StatusCode(), service.input, response.Body())
	}
}
