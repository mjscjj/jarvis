package insight

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LogReader tails one or more server log files for the debug panel and merges
// them by timestamp. cron 运行结果写在 stderr，Hertz/启动日志写在 stdout，
// 两个文件都要读才看得全。每个文件只读尾部 maxTailBytes，避免大日志撑爆内存。
type LogReader struct {
	paths []string
}

const maxTailBytes = 256 * 1024

// The process has two timestamp formats: our cron/pipeline loggers use slashes,
// while Hertz uses dashes and a comma before milliseconds. Both may have a
// module/level prefix (and Hertz may add ANSI color bytes), so search anywhere
// in the line instead of requiring the timestamp at byte zero.
var slashTS = regexp.MustCompile(`(?:^|\s)(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?)`)
var hertzTS = regexp.MustCompile(`(?:^|\s)(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2},\d{3})`)
var cronJobLine = regexp.MustCompile(`^([a-z][a-z-]*)-cron\s+\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?\s+(.*)$`)
var jobNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func NewLogReader(paths []string) (*LogReader, error) {
	cleaned := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.TrimSpace(p) != "" {
			cleaned = append(cleaned, p)
		}
	}
	if len(cleaned) == 0 {
		return nil, fmt.Errorf("log reader needs at least one path")
	}
	return &LogReader{paths: cleaned}, nil
}

// LogLine is one merged log line tagged with its originating file.
type LogLine struct {
	Source string `json:"source"` // 文件名（如 jarvis-server.error.log）
	Time   string `json:"time"`   // 解析出的时间戳，无法解析时为空
	Text   string `json:"text"`   // 原始整行
}

// LogTail is the log sub-tab payload.
type LogTail struct {
	Sources   []string  `json:"sources"`   // 参与归并的文件名
	Lines     []LogLine `json:"lines"`     // 已按时间归并的行（旧→新）
	Truncated bool      `json:"truncated"` // 任一文件超过尾读上限时为 true
	Notes     []string  `json:"notes"`     // 缺失文件等提示
}

// SystemTaskRun is one scheduler execution parsed directly from the process
// logs. System-task history intentionally has no database table: the launchd
// stdout/stderr files remain the only source of truth.
type SystemTaskRun struct {
	Time       string            `json:"time"`
	Source     string            `json:"source"`
	Module     string            `json:"module"`
	Job        string            `json:"job"`
	Status     string            `json:"status"`
	DurationMS *int64            `json:"duration_ms"`
	Fields     map[string]string `json:"fields"`
	Raw        string            `json:"raw"`
}

// parsedLine carries the sort key alongside the rendered line.
type parsedLine struct {
	line   LogLine
	ts     time.Time
	hasTS  bool
	fileIx int // 稳定排序用：同一时间戳保持文件内相对顺序
	lineIx int
}

// Tail returns up to `lines` most recent merged log lines across all files.
func (r *LogReader) Tail(lines int) (*LogTail, error) {
	if lines <= 0 || lines > 5000 {
		lines = 500
	}
	result := &LogTail{Sources: []string{}, Lines: []LogLine{}, Notes: []string{}}

	var parsed []parsedLine
	for fileIx, path := range r.paths {
		// 续行只能继承同一文件里的时间。跨 stdout/stderr 继承会把一个文件
		// 开头的无时间戳 SDK 日志错误地挂到另一个文件的最后事件上。
		var lastTS time.Time
		base := filepath.Base(path)
		result.Sources = append(result.Sources, base)

		raw, truncated, err := readTail(path)
		if err != nil {
			if os.IsNotExist(err) {
				result.Notes = append(result.Notes, fmt.Sprintf("%s 不存在（若非 launchd 启动，确认输出已重定向到此路径）", path))
				continue
			}
			return nil, err
		}
		if truncated {
			result.Truncated = true
		}
		for lineIx, text := range raw {
			ts, ok := parseLineTime(text)
			if ok {
				lastTS = ts
			}
			parsed = append(parsed, parsedLine{
				line:   LogLine{Source: base, Time: formatTS(ts, ok), Text: text},
				ts:     tsOrFallback(ts, ok, lastTS),
				hasTS:  ok,
				fileIx: fileIx,
				lineIx: lineIx,
			})
		}
	}

	sort.SliceStable(parsed, func(i, j int) bool {
		if !parsed[i].ts.Equal(parsed[j].ts) {
			return parsed[i].ts.Before(parsed[j].ts)
		}
		if parsed[i].fileIx != parsed[j].fileIx {
			return parsed[i].fileIx < parsed[j].fileIx
		}
		return parsed[i].lineIx < parsed[j].lineIx
	})

	if len(parsed) > lines {
		parsed = parsed[len(parsed)-lines:]
	}
	out := make([]LogLine, len(parsed))
	for i := range parsed {
		out[i] = parsed[i].line
	}
	result.Lines = out
	return result, nil
}

// SystemTaskRuns returns the latest log-backed executions for one exact job,
// newest first. It reads a bounded log tail, so log rotation naturally defines
// the retention period.
func (r *LogReader) SystemTaskRuns(job string, limit int) ([]SystemTaskRun, *LogTail, error) {
	job = strings.TrimSpace(job)
	if !jobNamePattern.MatchString(job) {
		return nil, nil, fmt.Errorf("system task job must match %s", jobNamePattern.String())
	}
	if limit <= 0 || limit > 500 {
		return nil, nil, fmt.Errorf("system task run limit must be between 1 and 500")
	}
	tail, err := r.Tail(5000)
	if err != nil {
		return nil, nil, err
	}
	runs := make([]SystemTaskRun, 0, limit)
	for i := len(tail.Lines) - 1; i >= 0 && len(runs) < limit; i-- {
		line := tail.Lines[i]
		match := cronJobLine.FindStringSubmatch(line.Text)
		if match == nil {
			continue
		}
		fields := map[string]string{}
		for _, pair := range kvPair.FindAllStringSubmatch(match[2], -1) {
			fields[pair[1]] = pair[2]
		}
		if fields["job"] != job {
			continue
		}
		if fullError := errorMessage(match[2]); fullError != "" {
			fields["error"] = fullError
		}
		durationMS, err := optionalNonNegativeInt64(fields, "duration_ms")
		if err != nil {
			return nil, nil, fmt.Errorf("parse system task run job=%s time=%s: %w", job, line.Time, err)
		}
		runs = append(runs, SystemTaskRun{
			Time: line.Time, Source: line.Source, Module: match[1], Job: job,
			Status: fields["status"], DurationMS: durationMS, Fields: fields, Raw: line.Text,
		})
	}
	return runs, tail, nil
}

func optionalNonNegativeInt64(fields map[string]string, key string) (*int64, error) {
	raw, ok := fields[key]
	if !ok {
		return nil, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer, got %q: %w", key, raw, err)
	}
	if value < 0 {
		return nil, fmt.Errorf("%s must not be negative, got %d", key, value)
	}
	return &value, nil
}

// readTail returns the last maxTailBytes of a file as lines (dropping a leading
// partial line when truncated).
func readTail(path string) ([]string, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("stat log file %s: %w", path, err)
	}
	var offset int64
	truncated := false
	if info.Size() > maxTailBytes {
		offset = info.Size() - maxTailBytes
		truncated = true
	}
	if _, err := file.Seek(offset, 0); err != nil {
		return nil, false, fmt.Errorf("seek log file %s: %w", path, err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	all := make([]string, 0, 1024)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("read log file %s: %w", path, err)
	}
	if truncated && len(all) > 0 {
		all = all[1:] // 半行丢弃
	}
	return all, truncated, nil
}

// parseLineTime extracts the log line timestamp, accepting an optional cron
// prefix word before it.
func parseLineTime(text string) (time.Time, bool) {
	if m := slashTS.FindStringSubmatch(text); m != nil {
		layout := "2006/01/02 15:04:05"
		value := m[1]
		if strings.Contains(value, ".") {
			layout = "2006/01/02 15:04:05.000000"
		}
		ts, err := time.ParseInLocation(layout, value, time.Local)
		return ts, err == nil
	}
	if m := hertzTS.FindStringSubmatch(text); m != nil {
		ts, err := time.ParseInLocation("2006-01-02 15:04:05,000", m[1], time.Local)
		return ts, err == nil
	}
	return time.Time{}, false
}

func tsOrFallback(ts time.Time, ok bool, fallback time.Time) time.Time {
	if ok {
		return ts
	}
	return fallback
}

func formatTS(ts time.Time, ok bool) string {
	if !ok {
		return ""
	}
	return ts.Format("2006-01-02 15:04:05.000")
}
