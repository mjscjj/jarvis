package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"jarvis/internal/observability"
	"jarvis/internal/okrworkspace/domain"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// LegacyOKROwners is an ingress-only snapshot of verified migration identities.
// Business models and persistence continue to use email exclusively.
type LegacyOKROwners map[string]domain.PersonRef

func LoadLegacyOKROwners(path string) (LegacyOKROwners, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var snapshot struct {
		People map[string]struct {
			domain.PersonRef
			SourceAppID string `json:"source_app_id"`
		} `json:"people"`
	}
	if err := json.Unmarshal(body, &snapshot); err != nil {
		return nil, err
	}
	if len(snapshot.People) == 0 {
		return nil, fmt.Errorf("legacy OKR owner mapping is empty")
	}
	result := make(LegacyOKROwners, len(snapshot.People))
	for id, person := range snapshot.People {
		if !strings.HasPrefix(id, "ou_") || person.SourceAppID == "" || !domain.ValidEmail(person.Email) || strings.TrimSpace(person.Name) == "" {
			return nil, fmt.Errorf("invalid legacy OKR owner mapping for %q", id)
		}
		person.Email = domain.NormalizeEmail(person.Email)
		result[id] = person.PersonRef
	}
	return result, nil
}

func (people LegacyOKROwners) Middleware() app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		path, method := string(c.Path()), string(c.Method())
		if (strings.HasPrefix(path, "/api/okr/") || strings.HasPrefix(path, "/api/biz-okr/")) &&
			(method == "POST" || method == "PUT" || method == "PATCH") && json.Valid(c.Request.Body()) {
			body, mapped, dropped := people.normalize(c.Request.Body())
			if mapped+dropped > 0 {
				c.Request.SetBody(body)
				hlog.CtxInfof(ctx, "legacy OKR owners normalized logid=%s path=%s mapped=%d dropped=%d", observability.LogID(ctx), path, mapped, dropped)
			}
		}
		c.Next(ctx)
	}
}

func (people LegacyOKROwners) normalize(body []byte) ([]byte, int, int) {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil || object == nil {
		return body, 0, 0
	}
	mapped, dropped := 0, 0
	var owners []json.RawMessage
	if raw, exists := object["owners"]; exists && json.Unmarshal(raw, &owners) == nil && owners != nil {
		kept := make([]json.RawMessage, 0, len(owners))
		for _, owner := range owners {
			var fields map[string]json.RawMessage
			if json.Unmarshal(owner, &fields) != nil || fields["open_id"] == nil {
				kept = append(kept, owner)
				continue
			}
			var email, id string
			_ = json.Unmarshal(fields["email"], &email)
			_ = json.Unmarshal(fields["open_id"], &id)
			delete(fields, "open_id")
			if strings.TrimSpace(email) == "" {
				person, found := people[strings.TrimSpace(id)]
				if !found {
					dropped++
					continue
				}
				fields["email"], _ = json.Marshal(person.Email)
				fields["name"], _ = json.Marshal(person.Name)
				// Do not retain an unrelated legacy union_id alongside a mapped email.
				delete(fields, "union_id")
				if person.UnionID != "" {
					fields["union_id"], _ = json.Marshal(person.UnionID)
				}
			}
			mapped++
			next, _ := json.Marshal(fields)
			kept = append(kept, next)
		}
		if mapped+dropped > 0 {
			object["owners"], _ = json.Marshal(kept)
		}
	}
	// Traverse only OKR entity containers, never arbitrary document content,
	// source_payload, comments, metrics or other embedded user JSON.
	if raw, exists := object["objective"]; exists {
		next, m, d := people.normalize(raw)
		if m+d > 0 {
			object["objective"] = next
			mapped, dropped = mapped+m, dropped+d
		}
	}
	for _, key := range []string{"objectives", "krs", "points"} {
		var items []json.RawMessage
		if json.Unmarshal(object[key], &items) != nil {
			continue
		}
		changed := false
		for i, item := range items {
			next, m, d := people.normalize(item)
			if m+d > 0 {
				items[i], changed = next, true
				mapped, dropped = mapped+m, dropped+d
			}
		}
		if changed {
			object[key], _ = json.Marshal(items)
		}
	}
	if mapped+dropped == 0 {
		return body, 0, 0
	}
	// RawMessage preserves numeric versions exactly, including large integers.
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(object)
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), mapped, dropped
}
