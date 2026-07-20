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
