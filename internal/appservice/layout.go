package appservice

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultAddress        = "127.0.0.1:18800"
	defaultStartupTimeout = 30 * time.Second
)

type Options struct {
	ResourceRoot    string
	StateRoot       string
	Address         string
	QdrantHealthURL string
	SupervisorPID   int
	StartupTimeout  time.Duration
}

type Layout struct {
	ResourceRoot    string
	StateRoot       string
	RuntimeRoot     string
	ConfigPath      string
	ServerBinary    string
	QdrantBinary    string
	QdrantConfig    string
	QdrantWorking   string
	LogDirectory    string
	ServerStdoutLog string
	ServerStderrLog string
	QdrantStdoutLog string
	QdrantStderrLog string
	LockPath        string
}

type RuntimeConnection struct {
	HTTPURL  string `json:"httpUrl"`
	DataRoot string `json:"dataRoot"`
}

func ResolveOptions(options Options) (Options, error) {
	var err error
	if strings.TrimSpace(options.ResourceRoot) == "" {
		options.ResourceRoot, err = defaultResourceRoot()
		if err != nil {
			return Options{}, err
		}
	}
	if strings.TrimSpace(options.StateRoot) == "" {
		options.StateRoot, err = defaultStateRoot()
		if err != nil {
			return Options{}, err
		}
	}
	if strings.TrimSpace(options.Address) == "" {
		options.Address = defaultAddress
	}
	host, port, err := net.SplitHostPort(options.Address)
	if err != nil {
		return Options{}, fmt.Errorf("parse desktop listen address %q: %w", options.Address, err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return Options{}, fmt.Errorf("desktop listen address must use loopback, got %q", options.Address)
	}
	if port == "" || port == "0" {
		return Options{}, fmt.Errorf("desktop listen address must use a fixed non-zero port")
	}
	if strings.TrimSpace(options.QdrantHealthURL) == "" {
		options.QdrantHealthURL = "http://127.0.0.1:6333/healthz"
	}
	if options.StartupTimeout <= 0 {
		options.StartupTimeout = defaultStartupTimeout
	}
	options.ResourceRoot, err = filepath.Abs(options.ResourceRoot)
	if err != nil {
		return Options{}, fmt.Errorf("resolve resource root: %w", err)
	}
	options.StateRoot, err = filepath.Abs(options.StateRoot)
	if err != nil {
		return Options{}, fmt.Errorf("resolve state root: %w", err)
	}
	return options, nil
}

func NewLayout(options Options) Layout {
	runtimeRoot := filepath.Join(options.StateRoot, "runtime")
	logDirectory := filepath.Join(options.StateRoot, "logs")
	return Layout{
		ResourceRoot:    options.ResourceRoot,
		StateRoot:       options.StateRoot,
		RuntimeRoot:     runtimeRoot,
		ConfigPath:      filepath.Join(runtimeRoot, "conf", "config.yaml"),
		ServerBinary:    filepath.Join(options.ResourceRoot, "bin", "jarvis-server"),
		QdrantBinary:    filepath.Join(options.ResourceRoot, "bin", "qdrant"),
		QdrantConfig:    filepath.Join(runtimeRoot, "conf", "qdrant.yaml"),
		QdrantWorking:   filepath.Join(runtimeRoot, "var", "qdrant"),
		LogDirectory:    logDirectory,
		ServerStdoutLog: filepath.Join(logDirectory, "jarvis-server.log"),
		ServerStderrLog: filepath.Join(logDirectory, "jarvis-server.error.log"),
		QdrantStdoutLog: filepath.Join(logDirectory, "qdrant.log"),
		QdrantStderrLog: filepath.Join(logDirectory, "qdrant.error.log"),
		LockPath:        filepath.Join(options.StateRoot, "app-service.pid"),
	}
}

func (l Layout) Connection(address string) RuntimeConnection {
	return RuntimeConnection{
		HTTPURL:  "http://" + address,
		DataRoot: l.StateRoot,
	}
}

func defaultResourceRoot() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve app service executable: %w", err)
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve app service symlinks: %w", err)
	}
	direct := filepath.Dir(filepath.Dir(executable))
	if resourceRootExists(direct) {
		return direct, nil
	}
	contents := direct
	bundled := filepath.Join(contents, "Resources", "runtime")
	if resourceRootExists(bundled) {
		return bundled, nil
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve development resource root: %w", err)
	}
	return workingDirectory, nil
}

func resourceRootExists(path string) bool {
	for _, required := range []string{
		filepath.Join(path, "conf", "config.yaml"),
		filepath.Join(path, "web", "dist", "index.html"),
	} {
		info, err := os.Stat(required)
		if err != nil || !info.Mode().IsRegular() {
			return false
		}
	}
	return true
}

func defaultStateRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, "Library", "Application Support", "Jarvis"), nil
}
