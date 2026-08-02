package insight

import (
	"os"
	"path/filepath"
	"testing"
)

func newRuntimeDebugService(t *testing.T, content string) *DebugService {
	t.Helper()
	logPath := filepath.Join(t.TempDir(), "jarvis-server.error.log")
	if err := os.WriteFile(logPath, []byte(content), 0o600); err != nil {
		t.Fatalf("write runtime log: %v", err)
	}
	reader, err := NewLogReader([]string{logPath})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}
	return &DebugService{logs: reader}
}

func TestFailuresParsesPipelineStagesAndCron(t *testing.T) {
	svc := newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-m3 stage=m3 trigger=realtime chat_id=oc_failed status=error error=decode JSON: EOF\n"+
		"pipeline 2026/07/24 15:44:01.000000 logid=log-m5-execute stage=m5 step=execute trigger=queue task_id=78 version=0 status=error error=execute Task id=78: enrichments[2] content is blank\n"+
		"pipeline 2026/07/24 15:46:01.000000 logid=log-stale stage=m5 trigger=queue task_id=79 status=stale\n"+
		"meeting-capture-cron 2026/07/24 15:47:01.000000 logid=log-cron job=meeting_minutes status=error error=permission check failed\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("Failures() len = %d, want 3: %+v", len(events), events)
	}

	byLogID := make(map[string]FailureEvent, len(events))
	for _, event := range events {
		byLogID[event.LogID] = event
	}
	assertFailureScope(t, byLogID["log-m3"], "extract", "m3", "chat_id", "oc_failed")
	assertFailureScope(t, byLogID["log-m5-execute"], "execute", "m5", "task_id", "78")
	assertFailureScope(t, byLogID["log-cron"], "meeting-capture", "", "job", "meeting_minutes")
}

func TestFailuresOnlySameScopeCanRecover(t *testing.T) {
	svc := newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-error stage=m3 trigger=realtime chat_id=oc_failed status=error error=decode JSON: EOF\n"+
		"pipeline 2026/07/24 15:43:01.000000 logid=log-other-ok stage=m3 trigger=realtime chat_id=oc_other status=ok created=1\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Failures() len = %d, want 1", len(events))
	}
	if events[0].Recovered {
		t.Fatalf("different chat success recovered failure: %+v", events[0])
	}

	svc = newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-error stage=m3 trigger=realtime chat_id=oc_failed status=error error=decode JSON: EOF\n"+
		"pipeline 2026/07/24 15:44:01.000000 logid=log-same-ok stage=m3 trigger=realtime chat_id=oc_failed status=ok created=1\n")
	events, err = svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() after same-scope success error = %v", err)
	}
	if len(events) != 1 || !events[0].Recovered {
		t.Fatalf("same chat success did not recover failure: %+v", events)
	}
}

func TestPipelineScopeIgnoresKVTokensInsideErrorText(t *testing.T) {
	svc := newRuntimeDebugService(t,
		"pipeline 2026/07/24 10:42:53.469073 logid=log-m5 stage=m5 step=execute trigger=realtime task_id=89 version=0 status=error error=execute Task id=89 failed task_id=90\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Failures() len = %d, want 1: %+v", len(events), events)
	}
	if events[0].ScopeID != "89" {
		t.Fatalf("M5 execution ScopeID = %q, want structured prefix task_id=89", events[0].ScopeID)
	}
}

func TestFailuresMergesRepeatedSameScopeAndSummary(t *testing.T) {
	svc := newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-first stage=m5 trigger=queue task_id=78 status=error error=execute Task id=78: enrichments[2] content is blank\n"+
		"pipeline 2026/07/24 15:43:01.000000 logid=log-second stage=m5 trigger=queue task_id=78 status=error error=execute Task id=78: enrichments[2] content is blank\n"+
		"pipeline 2026/07/24 15:44:01.000000 logid=log-other stage=m5 trigger=queue task_id=79 status=error error=execute Task id=79: timeout\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Failures() len = %d, want 2: %+v", len(events), events)
	}
	var repeated FailureEvent
	for _, event := range events {
		if event.ScopeID == "78" {
			repeated = event
		}
	}
	if repeated.Count != 2 {
		t.Fatalf("repeated Count = %d, want 2: %+v", repeated.Count, repeated)
	}
	if repeated.LogID != "log-second" || repeated.Time != "2026-07-24 15:43:01.000" {
		t.Fatalf("repeated event must retain latest occurrence: %+v", repeated)
	}
}

func TestFailuresDoesNotMergeSameErrorAcrossRecovery(t *testing.T) {
	svc := newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-first stage=m3 trigger=realtime chat_id=oc_1 status=error error=decode JSON: EOF\n"+
		"pipeline 2026/07/24 15:43:01.000000 logid=log-ok stage=m3 trigger=realtime chat_id=oc_1 status=ok created=1\n"+
		"pipeline 2026/07/24 15:44:01.000000 logid=log-second stage=m3 trigger=realtime chat_id=oc_1 status=error error=decode JSON: EOF\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Failures() len = %d, want two error episodes: %+v", len(events), events)
	}
	if events[0].LogID != "log-second" || events[0].Recovered {
		t.Fatalf("latest episode = %+v, want open log-second", events[0])
	}
	if events[1].LogID != "log-first" || !events[1].Recovered {
		t.Fatalf("older episode = %+v, want recovered log-first", events[1])
	}
}

func TestFailuresTreatsScopedPipelinePanicAsError(t *testing.T) {
	svc := newRuntimeDebugService(t,
		"pipeline 2026/07/24 15:42:01.000000 logid=log-panic stage=m3 trigger=realtime chat_id=oc_failed panic: runtime error: index out of range\n")

	events, err := svc.Failures(1000, 0)
	if err != nil {
		t.Fatalf("Failures() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Failures() len = %d, want 1: %+v", len(events), events)
	}
	if events[0].LogID != "log-panic" || events[0].ScopeID != "oc_failed" {
		t.Fatalf("panic event = %+v", events[0])
	}
}

func TestModulesIncludesPipelineStagesAndHyphenatedCron(t *testing.T) {
	svc := newRuntimeDebugService(t, ""+
		"pipeline 2026/07/24 15:42:01.000000 logid=log-m3 stage=m3 trigger=realtime chat_id=oc_1 status=error error=decode JSON: EOF\n"+
		"pipeline 2026/07/24 15:43:01.000000 logid=log-m3-ok stage=m3 trigger=realtime chat_id=oc_1 status=ok created=1\n"+
		"meeting-capture-cron 2026/07/24 15:44:01.000000 logid=log-cron job=meeting_minutes status=ok imported=1\n")

	runs, err := svc.Modules(1000)
	if err != nil {
		t.Fatalf("Modules() error = %v", err)
	}
	byModule := make(map[string]ModuleRun, len(runs))
	for _, run := range runs {
		byModule[run.Module] = run
	}
	if run := byModule["extract"]; !run.CurrentOK || run.Failures != 1 || run.Runs != 2 {
		t.Fatalf("extract module = %+v, want recovered latest run with one historical failure", run)
	}
	if run := byModule["meeting-capture"]; !run.CurrentOK || run.Job != "meeting_minutes" {
		t.Fatalf("meeting-capture module = %+v, want hyphenated cron module", run)
	}
}

func assertFailureScope(t *testing.T, event FailureEvent, module, stage, scopeType, scopeID string) {
	t.Helper()
	if event.Module != module || event.Stage != stage || event.ScopeType != scopeType || event.ScopeID != scopeID {
		t.Fatalf("failure scope = %+v, want module=%s stage=%s %s=%s", event, module, stage, scopeType, scopeID)
	}
	if event.Count != 1 {
		t.Fatalf("failure count = %d, want 1: %+v", event.Count, event)
	}
}
