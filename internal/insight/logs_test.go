package insight

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLineTimeSupportsCronAndHertz(t *testing.T) {
	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "cron",
			line: "capture-cron 2026/08/02 22:49:36.308651 logid=x job=discover status=ok",
			want: "2026-08-02 22:49:36.308",
		},
		{
			name: "hertz with ansi",
			line: "\x1b[1;31mError 2026-08-02 22:48:32,867 v1(7) fs.go:856 HERTZ: failed\x1b[0m",
			want: "2026-08-02 22:48:32.867",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ts, ok := parseLineTime(test.line)
			if !ok {
				t.Fatalf("parseLineTime(%q) did not find a timestamp", test.line)
			}
			if got := formatTS(ts, true); got != test.want {
				t.Fatalf("parseLineTime(%q) = %s, want %s", test.line, got, test.want)
			}
		})
	}
}

func TestLogReaderTailMergesCronAndHertzChronologically(t *testing.T) {
	dir := t.TempDir()
	stdout := filepath.Join(dir, "server.log")
	stderr := filepath.Join(dir, "server.error.log")
	if err := os.WriteFile(stdout, []byte(
		"Trace 2026-08-02 22:48:32,867 request one\n"+
			"Trace 2026-08-02 22:50:00,123 request two\n",
	), 0o600); err != nil {
		t.Fatalf("write stdout fixture: %v", err)
	}
	if err := os.WriteFile(stderr, []byte(
		"capture-cron 2026/08/02 22:48:20.000001 job=scan status=ok\n"+
			"capture-cron 2026/08/02 22:49:36.000001 job=discover status=ok\n",
	), 0o600); err != nil {
		t.Fatalf("write stderr fixture: %v", err)
	}

	reader, err := NewLogReader([]string{stdout, stderr})
	if err != nil {
		t.Fatalf("NewLogReader() error = %v", err)
	}
	tail, err := reader.Tail(10)
	if err != nil {
		t.Fatalf("Tail() error = %v", err)
	}
	want := []string{
		"2026-08-02 22:48:20.000",
		"2026-08-02 22:48:32.867",
		"2026-08-02 22:49:36.000",
		"2026-08-02 22:50:00.123",
	}
	if len(tail.Lines) != len(want) {
		t.Fatalf("Tail() lines = %d, want %d", len(tail.Lines), len(want))
	}
	for i, line := range tail.Lines {
		if line.Time != want[i] {
			t.Fatalf("Tail() line[%d].time = %q, want %q", i, line.Time, want[i])
		}
	}
}
