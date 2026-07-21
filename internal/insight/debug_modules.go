package insight

import (
	"regexp"
	"sort"
	"strings"
	"time"
)

// ModuleRun is the most recent parsed cron run for one module.
type ModuleRun struct {
	Module    string            `json:"module"`     // capture / memory / extract / decide / execute
	Time      string            `json:"time"`       // 该模块最近一条日志的时间戳
	Status    string            `json:"status"`     // ok / error / unknown（无 status= 字段时）
	CurrentOK bool              `json:"current_ok"` // 最近一次运行是否 ok（判「当前是否有问题」的唯一依据）
	Job       string            `json:"job"`        // job= 值，如 scan_hot / memorize / extract
	Fields    map[string]string `json:"fields"`     // 该行解析出的全部 k=v
	Runs      int               `json:"runs"`       // 日志窗口里该模块出现的行数
	Failures  int               `json:"failures"`   // 窗口里该模块 status!=ok 的行数
	LastError string            `json:"last_error"` // 窗口里最近一条 status!=ok 的原始行（历史参考，非「当前有问题」）
	Raw       string            `json:"raw"`        // 最近一条原始日志行
}

// cronLine matches "<module>-cron 2026/07/19 23:22:38.998088 <rest...>".
var cronLine = regexp.MustCompile(`^([a-z]+)-cron\s+(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?)\s+(.*)$`)

// kvPair matches key=value tokens (value has no spaces, matching how the cron
// loggers format their structured lines).
var kvPair = regexp.MustCompile(`(\w+)=(\S+)`)

// Modules parses the merged log tail into a per-module latest-run table. It
// reads the same log files as the log sub-tab (cron output is on stderr), so it
// stays truthful to whatever the process actually logged — no separate writer.
func (s *DebugService) Modules(maxLines int) ([]ModuleRun, error) {
	tail, err := s.logs.Tail(maxLines)
	if err != nil {
		return nil, err
	}
	byModule := map[string]*ModuleRun{}
	for _, line := range tail.Lines {
		m := cronLine.FindStringSubmatch(line.Text)
		if m == nil {
			continue
		}
		module, rest := m[1], m[3]
		fields := map[string]string{}
		for _, kv := range kvPair.FindAllStringSubmatch(rest, -1) {
			fields[kv[1]] = kv[2]
		}
		status := fields["status"]
		if status == "" {
			status = "unknown"
		}
		run := byModule[module]
		if run == nil {
			run = &ModuleRun{Module: module, Fields: map[string]string{}}
			byModule[module] = run
		}
		run.Runs++
		// Lines arrive oldest→newest, so the last assignment wins as "latest".
		run.Time = line.Time
		run.Status = status
		run.CurrentOK = status == "ok"
		run.Job = fields["job"]
		run.Fields = fields
		run.Raw = line.Text
		if status != "ok" && status != "unknown" {
			run.Failures++
			run.LastError = strings.TrimSpace(line.Text)
		}
	}
	out := make([]ModuleRun, 0, len(byModule))
	for _, run := range byModule {
		out = append(out, *run)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Module < out[j].Module })
	return out, nil
}

// FailureEvent is one cron run that logged status=error, kept for the "近 24h
// 报错时间线". Recovered records whether the same module logged a later ok run,
// so a transient blip (network jitter that self-healed) is visually separable
// from something still broken.
type FailureEvent struct {
	Time      string `json:"time"`      // 报错发生时间戳（日志原样）
	Module    string `json:"module"`    // capture / memory / extract / decide / execute
	Job       string `json:"job"`       // job= 值
	Error     string `json:"error"`     // error= 字段（截断），拿不到就用整行
	Recovered bool   `json:"recovered"` // 该模块之后是否又有过 ok 运行（true=已自愈）
	Raw       string `json:"raw"`       // 原始日志行
}

// logTimeLayout matches the cron timestamp format "2006/01/02 15:04:05(.000000)".
const logTimeLayout = "2006/01/02 15:04:05"

// Failures returns every cron run that logged status=error within the last
// sinceHours, newest first. It reads the same merged log tail as Modules (cron
// output on stderr) — no separate error store — so it never drifts from what the
// process actually logged. Each event is tagged Recovered if its module later
// logged an ok run, letting the UI de-emphasise self-healed blips.
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
		when   time.Time
		hasTS  bool
		module string
		event  FailureEvent
	}
	var events []parsed
	// lastOKAfter[module] tracks the latest ok time seen; used after the pass to
	// decide Recovered. We record failures in order, then resolve recovery.
	lastOK := map[string]time.Time{}

	for _, line := range tail.Lines {
		m := cronLine.FindStringSubmatch(line.Text)
		if m == nil {
			continue
		}
		module, tsText, rest := m[1], m[2], m[3]
		fields := map[string]string{}
		for _, kv := range kvPair.FindAllStringSubmatch(rest, -1) {
			fields[kv[1]] = kv[2]
		}
		when, tsErr := time.ParseInLocation(logTimeLayout, tsText[:len(logTimeLayout)], time.Local)
		hasTS := tsErr == nil
		if hasTS && sinceHours > 0 && when.Before(cutoff) {
			continue
		}
		status := fields["status"]
		if status == "ok" {
			if hasTS {
				lastOK[module] = when
			}
			continue
		}
		if status != "error" {
			continue
		}
		// error= messages contain spaces, so the kvPair token map truncates them
		// at the first word. Grab everything after "error=" to end of line for the
		// timeline; fall back to the whole line if there is no error= field.
		errText := errorMessage(rest)
		if errText == "" {
			errText = strings.TrimSpace(line.Text)
		}
		events = append(events, parsed{
			when: when, hasTS: hasTS, module: module,
			event: FailureEvent{
				Time: line.Time, Module: module, Job: fields["job"],
				Error: truncate(errText, 500), Raw: strings.TrimSpace(line.Text),
			},
		})
	}

	// Resolve Recovered: a failure is recovered if the module logged an ok run at
	// a later timestamp. Without a parseable timestamp we cannot compare, so we
	// leave Recovered=false (conservative: show it as still-relevant).
	out := make([]FailureEvent, 0, len(events))
	for _, p := range events {
		if p.hasTS {
			if okTime, ok := lastOK[p.module]; ok && okTime.After(p.when) {
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
