package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestQueryResourcesRejectsBadArgs(t *testing.T) {
	// db is only touched after argument validation, so a tool without a db still
	// exercises the validation branches deterministically.
	tool := &QueryResourcesTool{timeout: time.Second, maxLimit: 20}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":0}`)); err == nil {
		t.Fatal("Invoke accepted non-positive limit")
	}
	if _, err := tool.Invoke(context.Background(), json.RawMessage(`{"project_id":null,"person_open_id":null,"principal_only":null,"keyword":null,"limit":10,"extra":1}`)); err == nil {
		t.Fatal("Invoke accepted unknown argument field")
	}
}
