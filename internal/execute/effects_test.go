package execute

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseEffectsOpenPayload locks in lenient parsing: known fields are pulled
// into codexEffect, leftover top-level keys and brand-new kinds survive (never
// rejected), and the round-trip JSON stays one flat object per effect for the UI.
func TestParseEffectsOpenPayload(t *testing.T) {
	msg := `{
	  "outcome":"completed","summary":"done","failure_reason":"","needs_followup":"",
	  "enrichments":[],
	  "effects":[
	    {"kind":"feishu_message","title":"已通知张三","url":"https://feishu.cn/x","target":"研发群","message_id":"om_123"},
	    {"kind":"brand_new_kind","weird_field":{"nested":true},"count":42}
	  ],
	  "waiting":null
	}`
	res, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(res.Effects) != 2 {
		t.Fatalf("want 2 effects, got %d", len(res.Effects))
	}
	if res.Effects[0].Kind != "feishu_message" || res.Effects[0].URL != "https://feishu.cn/x" {
		t.Fatalf("known fields lost: %+v", res.Effects[0])
	}
	if _, ok := res.Effects[0].Extra["message_id"]; !ok {
		t.Fatalf("extra message_id dropped: %+v", res.Effects[0].Extra)
	}
	if res.Effects[1].Kind != "brand_new_kind" {
		t.Fatalf("unknown kind lost: %+v", res.Effects[1])
	}
	if _, ok := res.Effects[1].Extra["weird_field"]; !ok {
		t.Fatalf("nested extra dropped: %+v", res.Effects[1].Extra)
	}
	if _, err := json.Marshal(res.Effects); err != nil {
		t.Fatalf("re-marshal effects: %v", err)
	}
}

// TestParseEffectsExtraJSONString covers the Structured Outputs-compatible path:
// metadata travels in the "extra" string as JSON text and is expanded into Extra.
func TestParseEffectsExtraJSONString(t *testing.T) {
	msg := `{
	  "outcome":"completed","summary":"done","failure_reason":"","needs_followup":"",
	  "enrichments":[],
	  "effects":[
	    {"kind":"feishu_message","title":"已通知","extra":"{\"message_id\":\"om_123\",\"chat_name\":\"研发群\"}"}
	  ],
	  "waiting":null
	}`
	res, err := parseExecutionResult(msg)
	if err != nil {
		t.Fatalf("parse err: %v", err)
	}
	if len(res.Effects) != 1 {
		t.Fatalf("want 1 effect, got %d", len(res.Effects))
	}
	if got := string(res.Effects[0].Extra["message_id"]); !strings.Contains(got, "om_123") {
		t.Fatalf("message_id not expanded from extra: %+v", res.Effects[0].Extra)
	}
	if got := string(res.Effects[0].Extra["chat_name"]); !strings.Contains(got, "研发群") {
		t.Fatalf("chat_name not expanded from extra: %+v", res.Effects[0].Extra)
	}
}

// TestEffectsSchemaForbidsAdditionalProperties guards against reintroducing
// additionalProperties:true on effects items — Codex Structured Outputs reject it.
func TestEffectsSchemaForbidsAdditionalProperties(t *testing.T) {
	for name, schema := range map[string]string{"execution": executionResultSchema, "propose": proposeResultSchema} {
		var root map[string]any
		if err := json.Unmarshal([]byte(schema), &root); err != nil {
			t.Fatalf("%s schema JSON: %v", name, err)
		}
		props := root["properties"].(map[string]any)
		effects := props["effects"].(map[string]any)
		items := effects["items"].(map[string]any)
		if items["additionalProperties"] != false {
			t.Fatalf("%s effects.items.additionalProperties want false, got %#v", name, items["additionalProperties"])
		}
		itemProps := items["properties"].(map[string]any)
		required, _ := items["required"].([]any)
		reqSet := map[string]bool{}
		for _, r := range required {
			reqSet[r.(string)] = true
		}
		for key := range itemProps {
			if !reqSet[key] {
				t.Fatalf("%s effects.items.required missing %q (Structured Outputs needs every property)", name, key)
			}
		}
		if _, ok := itemProps["extra"]; !ok {
			t.Fatalf("%s effects.items missing extra string field", name)
		}
	}
}

// TestNormalizeEffectsDropsEmpty verifies only fully-empty effects are dropped;
// an effect carrying only an unknown kind or only extra fields is kept.
func TestNormalizeEffectsDropsEmpty(t *testing.T) {
	in := []codexEffect{
		{},
		{Kind: "note"},
		{Extra: map[string]json.RawMessage{"x": json.RawMessage(`1`)}},
	}
	out := normalizeEffects(in)
	if len(out) != 2 {
		t.Fatalf("want 2 kept, got %d: %+v", len(out), out)
	}
}
