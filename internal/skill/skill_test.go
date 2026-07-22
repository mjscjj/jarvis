package skill

import (
	"reflect"
	"testing"
)

func TestParseMetadata(t *testing.T) {
	meta, err := parseMetadata([]byte("---\nname: feishu-send-message\ndescription: 发送飞书消息\n---\n\n# 正文\n"))
	if err != nil {
		t.Fatalf("parseMetadata() error = %v", err)
	}
	if meta.Name != "feishu-send-message" || meta.Description != "发送飞书消息" {
		t.Fatalf("metadata = %#v", meta)
	}
}

func TestNormalizeStages(t *testing.T) {
	stages, _, err := normalizeStages([]string{StageExecute, StageExtract, StageExecute})
	if err != nil {
		t.Fatalf("normalizeStages() error = %v", err)
	}
	if !reflect.DeepEqual(stages, []string{StageExtract, StageExecute}) {
		t.Fatalf("stages = %#v", stages)
	}
	if _, _, err := normalizeStages(nil); err == nil {
		t.Fatal("normalizeStages(nil) must fail")
	}
	if _, _, err := normalizeStages([]string{"M6"}); err == nil {
		t.Fatal("normalizeStages(unknown) must fail")
	}
}
