package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"jarvis/internal/observability"
	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
)

func TestLegacyOwnerSnapshot(t *testing.T) {
	people, err := LoadLegacyOKROwners("../../data/okr/legacy-owner-identities.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ou_fd7f2e5bfd26788a3bffa9d563ff8904", "ou_650c33c67d3278a327b394858d3b5a6a"} {
		if people[id].Email != "wu.tuo@bytedance.com" {
			t.Fatalf("missing verified Wu Tuo identity %s", id)
		}
	}
}

func TestLegacyOwnersNormalizeOnlyOwnerFields(t *testing.T) {
	people := LegacyOKROwners{"ou_known": {Email: "a@example.test", Name: "甲"}}
	input := `{"version":9007199254740993,"krs":[{"owners":[{"open_id":"ou_known","name":"旧姓名"},{"open_id":"ou_missing","name":"乙"},{"email":"b@example.test","name":"丙"}],"points":[{"owners":[{"open_id":"ou_missing","name":"乙"}],"title":"保留正文"}]}],"source_payload":{"owners":[{"open_id":"ou_known"}]}}`
	body, mapped, dropped := people.normalize([]byte(input))
	if mapped != 1 || dropped != 2 {
		t.Fatalf("mapped=%d dropped=%d", mapped, dropped)
	}
	var got struct {
		Version json.Number `json:"version"`
		KRs     []struct {
			Owners []domain.PersonRef `json:"owners"`
			Points []struct {
				Title  string
				Owners []domain.PersonRef
			} `json:"points"`
		} `json:"krs"`
		Source json.RawMessage `json:"source_payload"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got.Version.String() != "9007199254740993" || len(got.KRs[0].Owners) != 2 || got.KRs[0].Owners[0].Name != "甲" || got.KRs[0].Owners[0].Email != "a@example.test" || len(got.KRs[0].Points[0].Owners) != 0 || got.KRs[0].Points[0].Title != "保留正文" || !strings.Contains(string(got.Source), "open_id") {
		t.Fatalf("unexpected normalized body: %s", body)
	}
	for _, body := range []string{`{"owners":null}`, `{"title":"未改负责人"}`, `{"owners":[{"name":"仅姓名"}]}`} {
		next, m, d := people.normalize([]byte(body))
		if string(next) != body || m+d != 0 {
			t.Fatalf("unrelated data changed: %s", next)
		}
	}
	// An explicit modern email wins over a redundant legacy ID; unknown fields
	// remain available to the normal strict decoder instead of being swallowed.
	body, _, _ = people.normalize([]byte(`{"owners":[{"email":"b@example.test","name":"乙","open_id":"ou_known","typo":true}]}`))
	if !strings.Contains(string(body), "b@example.test") || !strings.Contains(string(body), "typo") || strings.Contains(string(body), "open_id") {
		t.Fatalf("mixed owner: %s", body)
	}
}

func TestLegacyOwnersMiddlewareLogsOriginalEvenWhenRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "requests.jsonl")
	logger, err := observability.NewAPIRequestLogger(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = logger.Close() })
	h := server.New()
	h.Use(logger.Middleware())
	h.Use((LegacyOKROwners{"ou_known": {Email: "a@example.test", Name: "甲"}}).Middleware())
	h.PATCH("/api/biz-okr/test", func(ctx context.Context, c *app.RequestContext) {
		var input struct {
			Title  string             `json:"title"`
			Owners []domain.PersonRef `json:"owners"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			c.Status(400)
			return
		}
		c.JSON(200, input)
	})
	for _, tc := range []struct {
		body   string
		status int
	}{
		{`{"title":"正文","owners":[{"open_id":"ou_known","name":"旧"},{"open_id":"ou_missing","name":"丢弃"}]}`, 200},
		{`{"title":"正文","owners":[{"open_id":"ou_missing","name":"丢弃"}]}`, 200},
		{`{"title":"完整失败正文","owners":[{"open_id":"ou_known"}],"extra":true}`, 400},
		{`{"title":"损坏 JSON"`, 400},
	} {
		response := ut.PerformRequest(h.Engine, "PATCH", "/api/biz-okr/test", &ut.Body{Body: strings.NewReader(tc.body), Len: len(tc.body)}).Result()
		if response.StatusCode() != tc.status {
			t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected request/result pair for all 4 calls, got %d", len(lines))
	}
	var record struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(lines[4]), &record); err != nil {
		t.Fatal(err)
	}
	if record.Body != `{"title":"完整失败正文","owners":[{"open_id":"ou_known"}],"extra":true}` {
		t.Fatalf("original body lost: %s", record.Body)
	}
}
