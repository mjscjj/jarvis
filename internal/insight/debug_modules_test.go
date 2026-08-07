package insight

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestModulesSeparatesCurrentFromHistory 验证「最近一次是否成功」与「窗口内是否
// 出现过失败」被区分开：execute 窗口里先失败后成功，最近一次应判为 CurrentOK 且
// Failures 计数保留；extract 最近一次失败应判为 !CurrentOK。
func TestModulesSeparatesCurrentFromHistory(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jarvis-server.error.log")
	content := "" +
		"execute-cron 2026/07/20 20:55:13.000000 job=execute status=error error=token too long\n" +
		"execute-cron 2026/07/20 21:00:00.000000 job=execute status=ok loaded=1 executed=1\n" +
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

	execute, ok := byModule["execute"]
	if !ok {
		t.Fatalf("execute run missing, got %+v", runs)
	}
	if !execute.CurrentOK {
		t.Fatalf("execute CurrentOK = false, want true (最近一次 21:00 是 ok)")
	}
	if execute.Failures != 1 {
		t.Fatalf("execute Failures = %d, want 1", execute.Failures)
	}
	if execute.LastError == "" {
		t.Fatalf("execute LastError should keep the historical failure for reference")
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

// TestFailuresTimelineTagsRecovery 验证近 N 小时报错时间线：execute 先失败后 ok
// 应标 Recovered=true（已自愈）；extract 最后一次是失败、之后无 ok 应标
// Recovered=false（仍需关注）。结果按时间倒序，最新在前。
func TestFailuresTimelineTagsRecovery(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jarvis-server.error.log")
	content := "" +
		"execute-cron 2026/07/20 20:55:13.000000 job=execute status=error error=token too long\n" +
		"execute-cron 2026/07/20 21:00:00.000000 job=execute status=ok loaded=1 executed=1\n" +
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
	// 时间倒序：extract(21:01) 应排在 execute(20:55) 前。
	if events[0].Module != "extract" {
		t.Fatalf("events[0].Module = %q, want extract (最新在前)", events[0].Module)
	}
	if events[0].Recovered {
		t.Fatalf("extract 最后一次失败后无 ok，Recovered 应为 false")
	}
	if events[1].Module != "execute" {
		t.Fatalf("events[1].Module = %q, want execute", events[1].Module)
	}
	if !events[1].Recovered {
		t.Fatalf("execute 失败后 21:00 有 ok，Recovered 应为 true")
	}
	if events[1].Error != "token too long" {
		t.Fatalf("execute Error = %q, want 'token too long' (取 error= 字段)", events[1].Error)
	}
}

func TestSystemTaskRunsFiltersExactJobAndSupportsHyphenatedModule(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "jarvis-server.error.log")
	content := "" +
		"meeting-capture-cron 2026/07/24 16:32:16.899462 logid=meeting job=meeting_minutes status=ok duration_ms=1250 discovered=1 imported=0\n" +
		"pipeline-cron 2026/07/24 16:33:16.002420 logid=execute job=execute_reconcile status=queued\n" +
		"meeting-capture-cron 2026/07/24 16:37:17.114470 logid=meeting2 job=meeting_minutes status=error error=permission denied for minute\n"
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}

	runs, tail, err := reader.SystemTaskRuns("meeting_minutes", 10)
	if err != nil {
		t.Fatalf("SystemTaskRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("SystemTaskRuns() len = %d, want 2", len(runs))
	}
	if runs[0].Module != "meeting-capture" || runs[0].Status != "error" {
		t.Fatalf("latest run = %#v", runs[0])
	}
	if runs[0].Fields["error"] != "permission denied for minute" {
		t.Fatalf("full error = %q", runs[0].Fields["error"])
	}
	if runs[1].Status != "ok" {
		t.Fatalf("older run = %#v", runs[1])
	}
	if runs[1].DurationMS == nil || *runs[1].DurationMS != 1250 {
		t.Fatalf("older run duration = %#v", runs[1].DurationMS)
	}
	if tail == nil || len(tail.Sources) != 1 {
		t.Fatalf("tail metadata = %#v", tail)
	}
}

func TestSystemTaskRunsRejectsUnknownJobSyntax(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "jarvis-server.error.log")
	if err := os.WriteFile(logPath, nil, 0o600); err != nil {
		t.Fatalf("write temp log: %v", err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}
	if _, _, err := reader.SystemTaskRuns("../server", 10); err == nil {
		t.Fatal("SystemTaskRuns() accepted invalid job")
	}
}

func TestSystemTaskRunsRejectsMalformedDuration(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "jarvis-server.error.log")
	if err := os.WriteFile(logPath, []byte("capture-cron 2026/08/08 10:00:00.000001 job=discover status=ok duration_ms=nope\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reader.SystemTaskRuns("discover", 10); err == nil || !strings.Contains(err.Error(), "duration_ms must be an integer") {
		t.Fatalf("SystemTaskRuns malformed duration error = %v", err)
	}
}
