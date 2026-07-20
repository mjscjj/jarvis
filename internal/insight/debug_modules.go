package insight

import (
	"regexp"
	"sort"
	"strings"
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
