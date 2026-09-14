package main

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/common/hlog"
)

// 被丢弃的覆盖键必须看得见：hlog 默认写 stderr，桌面壳把 jarvis-server 的 stderr
// 重定向进 var/log/jarvis-server.error.log，调试面板尾读的就是这个文件。
func TestLogDroppedRuntimeOverrideKeysWritesKeyPaths(t *testing.T) {
	var captured bytes.Buffer
	hlog.SetOutput(&captured)
	defer hlog.SetOutput(os.Stderr)

	logDroppedRuntimeOverrideKeys(context.Background(), "/tmp/conf/config.runtime.yaml", nil)
	if captured.Len() != 0 {
		t.Fatalf("没有被丢弃的键时不该写日志: %s", captured.String())
	}

	logDroppedRuntimeOverrideKeys(context.Background(), "/tmp/conf/config.runtime.yaml",
		[]string{"extract.open_todo_limit", "extract.recent_task_limit"})
	line := captured.String()
	for _, want := range []string{
		"config.runtime.yaml",
		"extract.open_todo_limit",
		"extract.recent_task_limit",
	} {
		if !strings.Contains(line, want) {
			t.Fatalf("日志缺少 %q:\n%s", want, line)
		}
	}
}
