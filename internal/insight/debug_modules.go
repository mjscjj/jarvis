package insight

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// ModuleRun is the most recent parsed runtime event for one module.
type ModuleRun struct {
	Module    string            `json:"module"`     // capture / memory / extract / execute
	Time      string            `json:"time"`       // 该模块最近一条日志的时间戳
	Status    string            `json:"status"`     // ok / error / unknown（无 status= 字段时）
	CurrentOK bool              `json:"current_ok"` // 最近一次运行是否没有报错
	Job       string            `json:"job"`        // job= 值，如 scan_hot / memorize / extract
	Fields    map[string]string `json:"fields"`     // 该行解析出的全部 k=v
	Runs      int               `json:"runs"`       // 日志窗口里该模块出现的行数
	Failures  int               `json:"failures"`   // 窗口里该模块 status!=ok 的行数
	LastError string            `json:"last_error"` // 窗口里最近一条 status!=ok 的原始行（历史参考，非「当前有问题」）
	Raw       string            `json:"raw"`        // 最近一条原始日志行
}

// cronLine matches "<module>-cron 2026/07/19 23:22:38.998088 <rest...>".
var cronLine = regexp.MustCompile(`^([a-z][a-z-]*)-cron\s+(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.*)$`)

// kvPair matches key=value tokens (value has no spaces, matching how the cron
// loggers format their structured lines).
var kvPair = regexp.MustCompile(`(\w+)=(\S+)`)

// Modules parses the merged log tail into a per-module latest-run table.
func (s *DebugService) Modules(maxLines int) ([]ModuleRun, error) {
	tail, err := s.logs.Tail(maxLines)
	if err != nil {
		return nil, err
	}
	byModule := map[string]*ModuleRun{}
	for _, line := range tail.Lines {
		event, ok := parseRuntimeEvent(line)
		if !ok {
			continue
		}
		status := event.Status
		if status == "" {
			status = "unknown"
		}
		run := byModule[event.Module]
		if run == nil {
			run = &ModuleRun{Module: event.Module, Fields: map[string]string{}}
			byModule[event.Module] = run
		}
		run.Runs++
		run.Time = event.Time
		run.Status = status
		run.CurrentOK = status != "error"
		run.Job = event.Job
		run.Fields = event.Fields
		run.Raw = event.Raw
		if status == "error" {
			run.Failures++
			run.LastError = event.Raw
		}
	}
	out := make([]ModuleRun, 0, len(byModule))
	for _, run := range byModule {
		out = append(out, *run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out, nil
}

// FailureEvent is one scoped runtime failure kept for the recent timeline.
type FailureEvent struct {
	Time      string `json:"time"`
	Module    string `json:"module"`
	Stage     string `json:"stage"`
	Job       string `json:"job"`
	Trigger   string `json:"trigger"`
	ScopeType string `json:"scope_type"`
	ScopeID   string `json:"scope_id"`
	LogID     string `json:"logid"`
	Error     string `json:"error"`
	Count     int    `json:"count"`
	Recovered bool   `json:"recovered"`
	Raw       string `json:"raw"`
}

// Failures returns scoped cron and pipeline failures, newest first.
func (s *DebugService) Failures(maxLines, sinceHours int) ([]FailureEvent, error) {
	tail, err := s.logs.Tail(maxLines)
	if err != nil {
		return nil, err
	}
	var cutoff time.Time
	if sinceHours > 0 {
		cutoff = time.Now().Add(-time.Duration(sinceHours) * time.Hour)
	}

	type parsed struct {
		when     time.Time
		hasTS    bool
		scopeKey string
		event    FailureEvent
	}
	type activeFailure struct {
		identity string
		index    int
	}
	var events []parsed
	activeByScope := map[string]activeFailure{}
	lastOK := map[string]time.Time{}

	for _, line := range tail.Lines {
		runtime, ok := parseRuntimeEvent(line)
		if !ok {
			continue
		}
		if runtime.HasTime && sinceHours > 0 && runtime.When.Before(cutoff) {
			continue
		}
		if runtime.Status == "ok" {
			if runtime.HasTime {
				lastOK[runtime.ScopeKey] = runtime.When
			}
			delete(activeByScope, runtime.ScopeKey)
			continue
		}
		if runtime.Status != "error" {
			continue
		}
		errText := runtime.Error
		if errText == "" {
			errText = runtime.Raw
		}
		summary := normalizedErrorSummary(errText)
		identity := runtime.ScopeKey + "\x00" + summary
		if active, exists := activeByScope[runtime.ScopeKey]; exists && active.identity == identity {
			events[active.index].when = runtime.When
			events[active.index].hasTS = runtime.HasTime
			events[active.index].event.Time = runtime.Time
			events[active.index].event.LogID = runtime.LogID
			events[active.index].event.Raw = runtime.Raw
			events[active.index].event.Count++
			continue
		}
		activeByScope[runtime.ScopeKey] = activeFailure{identity: identity, index: len(events)}
		events = append(events, parsed{
			when: runtime.When, hasTS: runtime.HasTime, scopeKey: runtime.ScopeKey,
			event: FailureEvent{
				Time: runtime.Time, Module: runtime.Module, Stage: runtime.Stage,
				Job: runtime.Job, Trigger: runtime.Trigger,
				ScopeType: runtime.ScopeType, ScopeID: runtime.ScopeID,
				LogID: runtime.LogID, Error: truncate(summary, 500),
				Count: 1, Raw: runtime.Raw,
			},
		})
	}

	out := make([]FailureEvent, 0, len(events))
	for _, p := range events {
		if p.hasTS {
			if okTime, ok := lastOK[p.scopeKey]; ok && okTime.After(p.when) {
				p.event.Recovered = true
			}
		}
		out = append(out, p.event)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Time > out[j].Time })
	return out, nil
}

// errorMessage returns the full text after "error=" (which may contain spaces)
// up to end of line, or "" if there is no error= field.
func errorMessage(rest string) string {
	idx := strings.Index(rest, "error=")
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(rest[idx+len("error="):])
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
