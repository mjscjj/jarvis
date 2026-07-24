package execute

import (
	"encoding/json"
	"testing"
)

// TestParseEffectsOpenPayload locks in the deliberately OPEN behavior of the
// effects payload: known fields are pulled into codexEffect, arbitrary extra
// fields and brand-new kinds survive parsing (never rejected, never dropped),
// and the round-trip JSON stays one flat object per effect for the UI.
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
