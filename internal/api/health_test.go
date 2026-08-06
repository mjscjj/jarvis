package api

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"jarvis/internal/observability"
)

func TestHealthFailureCarriesLogID(t *testing.T) {
	h := server.New()
	h.Use(observability.Middleware())
	h.GET("/healthz", Health(nil))

	response := ut.PerformRequest(h.Engine, "GET", "/healthz", nil).Result()
	if response.StatusCode() != consts.StatusServiceUnavailable {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	logID := string(response.Header.Peek(observability.HeaderLogID))
	if !regexp.MustCompile(`^\d{13}[0-9a-f]{16}$`).MatchString(logID) {
		t.Fatalf("response LogID = %q, want a generated LogID", logID)
	}
	var payload struct {
		LogID string `json:"logid"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if payload.LogID != logID {
		t.Fatalf("body LogID = %q, header LogID = %q", payload.LogID, logID)
	}
}

type stubVectorIndex struct {
	version string
	err     error
}

func (s stubVectorIndex) HealthCheck(context.Context) (string, error) {
	return s.version, s.err
}

func openReadinessDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return db
}

// A stopped Qdrant or an unresolvable CLI must degrade without turning into an
// outage, and a deliberately disabled dependency must not read as degraded.
func TestReadinessSeparatesOutageFromDegradation(t *testing.T) {
	healthyIndex := stubVectorIndex{version: "1.18.2"}
	// sh is executable on every supported host, so an "ok" here means the probe
	// resolved a real binary rather than that the check was skipped.
	resolvableBin := "sh"

	for _, testCase := range []struct {
		name           string
		withDB         bool
		targets        ReadinessTargets
		wantStatusCode int
		wantOverall    string
		wantStates     map[string]string
	}{
		{
			name:           "database down is an outage",
			withDB:         false,
			targets:        ReadinessTargets{VectorIndex: healthyIndex, LarkCLIBin: resolvableBin, AgentCLIBin: resolvableBin},
			wantStatusCode: consts.StatusServiceUnavailable,
			wantOverall:    "error",
			wantStates:     map[string]string{"database": "error"},
		},
		{
			name:           "every dependency reachable",
			withDB:         true,
			targets:        ReadinessTargets{VectorIndex: healthyIndex, LarkCLIBin: resolvableBin, AgentCLIBin: resolvableBin},
			wantStatusCode: consts.StatusOK,
			wantOverall:    "ok",
			wantStates:     map[string]string{"database": "ok", "vector_index": "ok", "lark_cli": "ok", "agent_cli": "ok"},
		},
		{
			name:           "unresolvable cli degrades",
			withDB:         true,
			targets:        ReadinessTargets{VectorIndex: healthyIndex, LarkCLIBin: "jarvis-absent-binary", AgentCLIBin: resolvableBin},
			wantStatusCode: consts.StatusOK,
			wantOverall:    "degraded",
			wantStates:     map[string]string{"database": "ok", "lark_cli": "error", "agent_cli": "ok"},
		},
		{
			name:           "unreachable vector store degrades",
			withDB:         true,
			targets:        ReadinessTargets{VectorIndex: stubVectorIndex{err: fmt.Errorf("connection refused")}, LarkCLIBin: resolvableBin, AgentCLIBin: resolvableBin},
			wantStatusCode: consts.StatusOK,
			wantOverall:    "degraded",
			wantStates:     map[string]string{"database": "ok", "vector_index": "error"},
		},
		{
			name:           "disabled vector store is not degraded",
			withDB:         true,
			targets:        ReadinessTargets{LarkCLIBin: resolvableBin, AgentCLIBin: resolvableBin},
			wantStatusCode: consts.StatusOK,
			wantOverall:    "ok",
			wantStates:     map[string]string{"vector_index": "disabled"},
		},
		{
			name:           "unconfigured cli degrades",
			withDB:         true,
			targets:        ReadinessTargets{VectorIndex: healthyIndex, LarkCLIBin: resolvableBin, AgentCLIBin: "  "},
			wantStatusCode: consts.StatusOK,
			wantOverall:    "degraded",
			wantStates:     map[string]string{"agent_cli": "error"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var db *gorm.DB
			if testCase.withDB {
				db = openReadinessDB(t)
			}
			h := server.New()
			h.Use(observability.Middleware())
			h.GET("/readyz", Readiness(db, testCase.targets))

			response := ut.PerformRequest(h.Engine, "GET", "/readyz", nil).Result()
			if response.StatusCode() != testCase.wantStatusCode {
				t.Fatalf("status = %d, want %d, body=%s", response.StatusCode(), testCase.wantStatusCode, response.Body())
			}
			var payload struct {
				Status       string `json:"status"`
				LogID        string `json:"logid"`
				Dependencies map[string]struct {
					Status  string `json:"status"`
					Error   string `json:"error"`
					Path    string `json:"path"`
					Version string `json:"version"`
				} `json:"dependencies"`
			}
			if err := json.Unmarshal(response.Body(), &payload); err != nil {
				t.Fatalf("decode readiness response: %v body=%s", err, response.Body())
			}
			if payload.Status != testCase.wantOverall {
				t.Fatalf("overall status = %q, want %q, body=%s", payload.Status, testCase.wantOverall, response.Body())
			}
			if payload.LogID == "" {
				t.Fatal("readiness response carries no LogID")
			}
			for name, wantState := range testCase.wantStates {
				state, ok := payload.Dependencies[name]
				if !ok {
					t.Fatalf("dependency %q missing from %s", name, response.Body())
				}
				if state.Status != wantState {
					t.Fatalf("dependency %q status = %q, want %q", name, state.Status, wantState)
				}
				if wantState == "error" && state.Error == "" {
					t.Fatalf("dependency %q reports error without a reason: %s", name, response.Body())
				}
			}
		})
	}
}

// The restart poller reads /healthz, so a stopped Qdrant or a missing CLI must
// not change what it sees.
func TestHealthIgnoresExternalDependencies(t *testing.T) {
	h := server.New()
	h.Use(observability.Middleware())
	h.GET("/healthz", Health(openReadinessDB(t)))

	response := ut.PerformRequest(h.Engine, "GET", "/healthz", nil).Result()
	if response.StatusCode() != consts.StatusOK {
		t.Fatalf("status = %d body=%s", response.StatusCode(), response.Body())
	}
	if regexp.MustCompile(`vector_index|lark_cli|agent_cli`).Match(response.Body()) {
		t.Fatalf("/healthz leaked an external dependency into the liveness contract: %s", response.Body())
	}
}
