package insight

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var processLinePattern = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+(\S+)\s+(.+)$`)

// AgentProcess is one logical Codex or Trae runtime. Launcher/helper processes
// are excluded so one runtime is counted once.
type AgentProcess struct {
	Kind        string `json:"kind"`
	Mode        string `json:"mode"`
	Source      string `json:"source"`
	PID         int    `json:"pid"`
	PPID        int    `json:"ppid"`
	PGID        int    `json:"pgid"`
	RootPID     int    `json:"root_pid"`
	Nested      bool   `json:"nested"`
	Elapsed     string `json:"elapsed"`
	JarvisOwned bool   `json:"jarvis_owned"`
	Command     string `json:"command"`
}

type AgentProcessSummary struct {
	CodexServices  int `json:"codex_services"`
	CodexExecuting int `json:"codex_executing"`
	TraeDesktop    int `json:"trae_desktop"`
	TraeCLI        int `json:"trae_cli"`
	JarvisCodex    int `json:"jarvis_codex"`
	JarvisTrae     int `json:"jarvis_trae"`
}

type AgentProcessSnapshot struct {
	SampledAt string              `json:"sampled_at"`
	Summary   AgentProcessSummary `json:"summary"`
	Items     []AgentProcess      `json:"items"`
}

type processRow struct {
	pid     int
	ppid    int
	pgid    int
	elapsed string
	command string
}

type agentClass struct {
	kind string
	mode string
}

// AgentProcesses returns a point-in-time operating-system process snapshot.
func (s *DebugService) AgentProcesses(ctx context.Context) (*AgentProcessSnapshot, error) {
	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,pgid=,etime=,args=").Output()
	if err != nil {
		return nil, fmt.Errorf("list processes: %w", err)
	}
	return buildAgentProcessSnapshot(string(output), os.Getpid(), time.Now()), nil
}

func buildAgentProcessSnapshot(output string, jarvisPID int, sampledAt time.Time) *AgentProcessSnapshot {
	rows := parseProcessRows(output)
	parents := make(map[int]int, len(rows))
	rowsByPID := make(map[int]processRow, len(rows))
	classes := make(map[int]agentClass)
	for _, row := range rows {
		parents[row.pid] = row.ppid
		rowsByPID[row.pid] = row
		kind, mode, ok := classifyAgentProcess(row.command)
		if ok {
			classes[row.pid] = agentClass{kind: kind, mode: mode}
		}
	}

	items := make([]AgentProcess, 0)
	for _, row := range rows {
		classification, ok := classes[row.pid]
		if !ok {
			continue
		}
		rootPID, nested := agentRoot(row.pid, classes, parents)
		items = append(items, AgentProcess{
			Kind:        classification.kind,
			Mode:        classification.mode,
			Source:      agentSource(row.pid, classification.kind, jarvisPID, rowsByPID, parents),
			PID:         row.pid,
			PPID:        row.ppid,
			PGID:        row.pgid,
			RootPID:     rootPID,
			Nested:      nested,
			Elapsed:     row.elapsed,
			JarvisOwned: descendsFrom(row.pid, jarvisPID, parents),
			Command:     row.command,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Kind != items[j].Kind {
			return items[i].Kind < items[j].Kind
		}
		if items[i].RootPID != items[j].RootPID {
			return items[i].RootPID < items[j].RootPID
		}
		if items[i].Nested != items[j].Nested {
			return !items[i].Nested
		}
		return items[i].PID < items[j].PID
	})

	snapshot := &AgentProcessSnapshot{
		SampledAt: sampledAt.Format(time.RFC3339),
		Items:     items,
	}
	codexServices := make(map[int]struct{})
	traeDesktops := make(map[int]struct{})
	traeCLIs := make(map[int]struct{})
	jarvisCodex := make(map[int]struct{})
	jarvisTrae := make(map[int]struct{})
	for _, item := range items {
		switch item.Kind {
		case "codex":
			if item.Mode == "app-server" {
				codexServices[item.RootPID] = struct{}{}
			}
			if item.Mode == "exec" {
				snapshot.Summary.CodexExecuting++
			}
			if item.JarvisOwned {
				jarvisCodex[item.RootPID] = struct{}{}
			}
		case "trae":
			switch item.Mode {
			case "desktop":
				traeDesktops[item.RootPID] = struct{}{}
			case "cli":
				traeCLIs[item.RootPID] = struct{}{}
			}
			if item.JarvisOwned {
				jarvisTrae[item.RootPID] = struct{}{}
			}
		}
	}
	snapshot.Summary.CodexServices = len(codexServices)
	snapshot.Summary.TraeDesktop = len(traeDesktops)
	snapshot.Summary.TraeCLI = len(traeCLIs)
	snapshot.Summary.JarvisCodex = len(jarvisCodex)
	snapshot.Summary.JarvisTrae = len(jarvisTrae)
	return snapshot
}

// agentRoot collapses nested agent runtimes into the outermost user-visible
// session. The nested process remains in Items for diagnostics.
func agentRoot(pid int, classes map[int]agentClass, parents map[int]int) (rootPID int, nested bool) {
	rootPID = pid
	seen := map[int]struct{}{pid: {}}
	current := pid
	for {
		parent, ok := parents[current]
		if !ok || parent <= 0 || parent == current {
			return rootPID, nested
		}
		if _, ok := seen[parent]; ok {
			return rootPID, nested
		}
		seen[parent] = struct{}{}
		if _, ok := classes[parent]; ok {
			rootPID = parent
			nested = true
		}
		current = parent
	}
}

// agentSource reports the owning application or daemon, not the binary bundle
// that happened to provide the codex executable.
func agentSource(pid int, kind string, jarvisPID int, rows map[int]processRow, parents map[int]int) string {
	if descendsFrom(pid, jarvisPID, parents) {
		return "jarvis"
	}

	var ccConnect, paseo, chatGPT, trae bool
	seen := map[int]struct{}{}
	current := pid
	for current > 0 {
		if _, ok := seen[current]; ok {
			break
		}
		seen[current] = struct{}{}
		row, ok := rows[current]
		if !ok {
			break
		}
		command := strings.ToLower(row.command)
		ccConnect = ccConnect || strings.Contains(command, "cc-connect")
		paseo = paseo || strings.Contains(command, "paseo daemon")
		chatGPT = chatGPT || strings.Contains(command, "/applications/chatgpt.app/")
		trae = trae || strings.Contains(command, "/applications/trae cn.app/")
		if row.ppid == current {
			break
		}
		current = row.ppid
	}

	switch {
	case ccConnect:
		return "cc-connect"
	case paseo:
		return "paseo"
	case chatGPT:
		return "chatgpt"
	case trae || kind == "trae":
		return "trae"
	default:
		return "other"
	}
}

func parseProcessRows(output string) []processRow {
	lines := strings.Split(output, "\n")
	rows := make([]processRow, 0, len(lines))
	for _, line := range lines {
		match := processLinePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		pid, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		ppid, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		pgid, err := strconv.Atoi(match[3])
		if err != nil {
			continue
		}
		rows = append(rows, processRow{
			pid: pid, ppid: ppid, pgid: pgid,
			elapsed: match[4], command: strings.TrimSpace(match[5]),
		})
	}
	return rows
}

func classifyAgentProcess(command string) (kind, mode string, ok bool) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", "", false
	}
	executable := filepath.Base(fields[0])
	if executable == "codex" {
		for _, field := range fields[1:] {
			switch field {
			case "exec":
				return "codex", "exec", true
			case "app-server":
				return "codex", "app-server", true
			}
		}
		return "", "", false
	}
	if executable == "traex" || executable == "trae" {
		return "trae", "cli", true
	}
	if strings.HasPrefix(command, "/Applications/Trae CN.app/Contents/MacOS/Electron") {
		return "trae", "desktop", true
	}
	return "", "", false
}

func descendsFrom(pid, rootPID int, parents map[int]int) bool {
	if rootPID <= 0 {
		return false
	}
	seen := map[int]struct{}{}
	for pid > 0 {
		if pid == rootPID {
			return true
		}
		if _, ok := seen[pid]; ok {
			return false
		}
		seen[pid] = struct{}{}
		parent, ok := parents[pid]
		if !ok || parent == pid {
			return false
		}
		pid = parent
	}
	return false
}
