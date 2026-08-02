package insight

import (
	"regexp"
	"strings"
	"time"
)

var pipelineLine = regexp.MustCompile(`^pipeline\s+\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?\s+(.*)$`)

type runtimeEvent struct {
	Time      string
	When      time.Time
	HasTime   bool
	Module    string
	Stage     string
	Status    string
	Job       string
	Trigger   string
	ScopeType string
	ScopeID   string
	ScopeKey  string
	LogID     string
	Error     string
	Fields    map[string]string
	Raw       string
}

func parseRuntimeEvent(line LogLine) (runtimeEvent, bool) {
	if match := cronLine.FindStringSubmatch(line.Text); match != nil {
		fields := parseRuntimeFields(match[3])
		status, errText := runtimeStatus(fields, match[3])
		job := strings.TrimSpace(fields["job"])
		scopeID := job
		if scopeID == "" {
			scopeID = "unknown"
		}
		when, hasTime := parseLineTime(line.Text)
		return runtimeEvent{
			Time: line.Time, When: when, HasTime: hasTime,
			Module: match[1], Status: status, Job: job,
			ScopeType: "job", ScopeID: scopeID,
			ScopeKey: "cron:" + match[1] + ":job=" + scopeID,
			LogID:    fields["logid"], Error: errText, Fields: fields,
			Raw: strings.TrimSpace(line.Text),
		}, true
	}

	match := pipelineLine.FindStringSubmatch(line.Text)
	if match == nil {
		return runtimeEvent{}, false
	}
	fields := parseRuntimeFields(match[1])
	stage := strings.TrimSpace(fields["stage"])
	module, ok := pipelineModule(stage)
	if !ok {
		return runtimeEvent{}, false
	}
	status, errText := runtimeStatus(fields, match[1])
	scopeType, scopeID, scopeKey := pipelineScope(stage, fields)
	when, hasTime := parseLineTime(line.Text)
	return runtimeEvent{
		Time: line.Time, When: when, HasTime: hasTime,
		Module: module, Stage: stage, Status: status,
		Trigger: fields["trigger"], ScopeType: scopeType, ScopeID: scopeID, ScopeKey: scopeKey,
		LogID: fields["logid"], Error: errText, Fields: fields,
		Raw: strings.TrimSpace(line.Text),
	}, true
}

func parseRuntimeFields(rest string) map[string]string {
	if index := strings.Index(rest, " error="); index >= 0 {
		rest = rest[:index]
	} else if strings.HasPrefix(rest, "error=") {
		rest = ""
	}
	fields := map[string]string{}
	for _, pair := range kvPair.FindAllStringSubmatch(rest, -1) {
		fields[pair[1]] = pair[2]
	}
	return fields
}

func runtimeStatus(fields map[string]string, rest string) (string, string) {
	status := strings.TrimSpace(fields["status"])
	errText := errorMessage(rest)
	lower := strings.ToLower(rest)
	if status == "" && (strings.Contains(lower, "panic") || strings.Contains(lower, "fatal")) {
		status = "error"
		if errText == "" {
			errText = strings.TrimSpace(rest)
		}
	}
	return status, errText
}

func pipelineModule(stage string) (string, bool) {
	switch stage {
	case "m3":
		return "extract", true
	case "m5":
		return "execute", true
	default:
		return "", false
	}
}

func pipelineScope(stage string, fields map[string]string) (string, string, string) {
	scopeType := ""
	scopeID := ""
	switch stage {
	case "m3":
		scopeType, scopeID = "chat_id", strings.TrimSpace(fields["chat_id"])
	case "m5":
		scopeType, scopeID = "task_id", strings.TrimSpace(fields["task_id"])
	}
	if scopeID == "" {
		scopeType, scopeID = "trigger", strings.TrimSpace(fields["trigger"])
	}
	if scopeID == "" {
		scopeID = "unknown"
	}
	return scopeType, scopeID, "pipeline:" + stage + ":" + scopeType + "=" + scopeID
}

func normalizedErrorSummary(value string) string {
	return strings.Join(strings.Fields(value), " ")
}
