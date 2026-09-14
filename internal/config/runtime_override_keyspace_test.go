package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// UpdateRuntimeOverride 写回前会剪掉 Config 不认识的键，于是覆盖文件的键空间被
// 收窄成 Config 的键空间。这里锁住这条隐含约束：给设置页加字段却忘了在 Config
// 里加对应字段，保存时会被当成未知键静默丢弃，用户看不到任何报错。

func assertPatchKeysKnown(t *testing.T, name string, patch any) {
	t.Helper()
	raw, err := yaml.Marshal(patch)
	if err != nil {
		t.Fatalf("marshal %s patch: %v", name, err)
	}
	var document yaml.Node
	if err := yaml.Unmarshal(raw, &document); err != nil {
		t.Fatalf("parse %s patch: %v", name, err)
	}
	if dropped := pruneUnknownRuntimeOverrideKeys(document.Content[0]); len(dropped) > 0 {
		t.Fatalf("%s patch 含 Config 不认识的键，保存时会被静默丢弃: %v", name, dropped)
	}
}

func TestRuntimeOverridePatchKeysAreKnownToConfig(t *testing.T) {
	assertPatchKeysKnown(t, "settings", runtimeOverrideFromSettings(RuntimeSettings{}))
	assertPatchKeysKnown(t, "security", map[string]any{
		"capture": map[string]any{
			"p2p_scan_enabled":       true,
			"auto_related_p2p_top_n": 20,
		},
	})
	// internal/onboarding 引导末尾写回的两段，键必须同样在 Config 里。
	assertPatchKeysKnown(t, "onboarding", map[string]any{
		"meeting_sweep": map[string]any{"enabled": true},
		"morning_brief": map[string]any{"enabled": true},
	})
}
