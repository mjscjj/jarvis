package insight

import (
	"os"
	"path/filepath"
	"testing"
)

// TestModulesSeparatesCurrentFromHistory 验证「最近一次是否成功」与「窗口内是否
// 出现过失败」被区分开：decide 窗口里先失败后成功，最近一次应判为 CurrentOK 且
// Failures 计数保留；extract 最近一次失败应判为 !CurrentOK。
func TestModulesSeparatesCurrentFromHistory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jarvis-server.error.log")
	content := "" +
		"decide-cron 2026/07/20 20:55:13.000000 job=decide status=error error=token too long\n" +
		"decide-cron 2026/07/20 21:00:00.000000 job=decide status=ok loaded=1 evaluated=1\n" +
		"extract-cron 2026/07/20 21:01:00.000000 job=extract status=error error=usage limit\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}
	svc := &DebugService{logs: reader}

	runs, err := svc.Modules(1000)
	if err != nil {
		t.Fatalf("Modules() error = %v", err)
	}
	byModule := map[string]ModuleRun{}
	for _, r := range runs {
		byModule[r.Module] = r
	}

	decide, ok := byModule["decide"]
	if !ok {
		t.Fatalf("decide run missing, got %+v", runs)
	}
	if !decide.CurrentOK {
		t.Fatalf("decide CurrentOK = false, want true (最近一次 21:00 是 ok)")
	}
	if decide.Failures != 1 {
		t.Fatalf("decide Failures = %d, want 1", decide.Failures)
	}
	if decide.LastError == "" {
		t.Fatalf("decide LastError should keep the historical failure for reference")
	}

	extract, ok := byModule["extract"]
	if !ok {
		t.Fatalf("extract run missing, got %+v", runs)
	}
	if extract.CurrentOK {
		t.Fatalf("extract CurrentOK = true, want false (最近一次是 error)")
	}
	if extract.Failures != 1 {
		t.Fatalf("extract Failures = %d, want 1", extract.Failures)
	}
}

// TestFailuresTimelineTagsRecovery 验证近 N 小时报错时间线：decide 先失败后 ok
// 应标 Recovered=true（已自愈）；extract 最后一次是失败、之后无 ok 应标
// Recovered=false（仍需关注）。结果按时间倒序，最新在前。
func TestFailuresTimelineTagsRecovery(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jarvis-server.error.log")
	content := "" +
		"decide-cron 2026/07/20 20:55:13.000000 job=decide status=error error=token too long\n" +
		"decide-cron 2026/07/20 21:00:00.000000 job=decide status=ok loaded=1 evaluated=1\n" +
		"extract-cron 2026/07/20 21:01:00.000000 job=extract status=error error=usage limit\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}
	svc := &DebugService{logs: reader}

	// sinceHours=0 关闭时间过滤，纯看历史 fixture（否则固定日期早已超 24h）。
	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Failures() len = %d, want 2 (两条 error 行)", len(events))
	}
	// 时间倒序：extract(21:01) 应排在 decide(20:55) 前。
	if events[0].Module != "extract" {
		t.Fatalf("events[0].Module = %q, want extract (最新在前)", events[0].Module)
	}
	if events[0].Recovered {
		t.Fatalf("extract 最后一次失败后无 ok，Recovered 应为 false")
	}
	if events[1].Module != "decide" {
		t.Fatalf("events[1].Module = %q, want decide", events[1].Module)
	}
	if !events[1].Recovered {
		t.Fatalf("decide 失败后 21:00 有 ok，Recovered 应为 true")
	}
	if events[1].Error != "token too long" {
		t.Fatalf("decide Error = %q, want 'token too long' (取 error= 字段)", events[1].Error)
	}
}
