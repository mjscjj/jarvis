package capture

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const messageEventReadyMarker = "[event] ready event_key=" + messageEventType

type MessageEventConsumerOptions struct {
	Bin          string
	Profile      string
	ReadyTimeout time.Duration
}

type messageEventAppender interface {
	AppendMessageEvent(context.Context, json.RawMessage) (*MessageEventResult, error)
}

// MessageEventConsumer owns the one unbounded lark-cli event process. stdin is
// intentionally kept open: lark-cli treats EOF as a graceful shutdown request.
type MessageEventConsumer struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	done     chan struct{}
	stopping atomic.Bool
	ready    atomic.Bool

	stopOnce sync.Once
	termOnce sync.Once
	errMu    sync.RWMutex
	err      error
}

// StartMessageEventConsumer blocks until lark-cli emits its documented ready
// marker. Startup failures are returned synchronously so Jarvis never serves as
// "realtime" while the event connection is actually absent.
func StartMessageEventConsumer(
	ctx context.Context,
	appender messageEventAppender,
	opts MessageEventConsumerOptions,
	logger *log.Logger,
) (*MessageEventConsumer, error) {
	if appender == nil {
		return nil, fmt.Errorf("message event appender is nil")
	}
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("message event lark-cli bin is empty")
	}
	if strings.TrimSpace(opts.Profile) == "" {
		return nil, fmt.Errorf("message event lark-cli profile is empty")
	}
	if opts.ReadyTimeout <= 0 {
		return nil, fmt.Errorf("message event ready timeout must be positive")
	}
	if logger == nil {
		return nil, fmt.Errorf("message event logger is nil")
	}

	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("resolve message event lark-cli binary %q: %w", opts.Bin, err)
	}
	cmd := exec.Command(
		bin,
		"--profile", opts.Profile,
		"event", "consume", messageEventType,
		"--as", "bot",
	)
	cmd.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open message event stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open message event stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open message event stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start message event consumer: %w", err)
	}

	consumer := &MessageEventConsumer{cmd: cmd, stdin: stdin, done: make(chan struct{})}
	ready := make(chan struct{})
	var readyOnce sync.Once
	var readers sync.WaitGroup
	diagnostics := &diagnosticBuffer{}

	readers.Add(1)
	go func() {
		defer readers.Done()
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !consumer.ready.Load() {
				diagnostics.Add(line)
			}
			if strings.HasPrefix(line, messageEventReadyMarker) {
				consumer.ready.Store(true)
				readyOnce.Do(func() { close(ready) })
				continue
			}
			logger.Printf("job=consume status=info diagnostic=%q", line)
		}
		if err := scanner.Err(); err != nil {
			diagnostics.Add("read stderr: " + err.Error())
		}
	}()

	readers.Add(1)
	go func() {
		defer readers.Done()
		decoder := json.NewDecoder(stdout)
		for {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				if !errors.Is(err, io.EOF) {
					logger.Printf("job=consume status=error error=decode event stream: %+v", err)
					consumer.stopOnce.Do(func() { _ = consumer.stdin.Close() })
				}
				return
			}
			result, err := appender.AppendMessageEvent(ctx, raw)
			if err != nil {
				logger.Printf("job=consume status=error error=append message event: %+v raw=%s", err, raw)
				continue
			}
			logger.Printf(
				"job=consume status=ok message_id=%s chat_id=%s inserted=%t related=%t",
				result.MessageID, result.ChatID, result.Inserted, result.Related,
			)
		}
	}()

	go func() {
		waitErr := cmd.Wait()
		readers.Wait()
		if !consumer.stopping.Load() && ctx.Err() == nil {
			if waitErr == nil {
				waitErr = fmt.Errorf("message event consumer exited unexpectedly")
			} else {
				waitErr = fmt.Errorf("message event consumer exited: %w", waitErr)
			}
			if detail := diagnostics.String(); detail != "" {
				waitErr = fmt.Errorf("%w; stderr=%s", waitErr, detail)
			}
			logger.Printf("job=consume status=error error=%+v", waitErr)
		} else {
			waitErr = nil
		}
		consumer.errMu.Lock()
		consumer.err = waitErr
		consumer.errMu.Unlock()
		close(consumer.done)
	}()

	go func() {
		select {
		case <-ctx.Done():
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := consumer.Stop(stopCtx); err != nil {
				logger.Printf("job=consume status=error error=stop message event consumer: %+v", err)
			}
		case <-consumer.done:
		}
	}()

	timer := time.NewTimer(opts.ReadyTimeout)
	defer timer.Stop()
	select {
	case <-ready:
		logger.Printf("job=consume status=ok state=ready profile=%s", opts.Profile)
		return consumer, nil
	case <-consumer.done:
		return nil, fmt.Errorf("message event consumer stopped before ready: %w", consumer.Err())
	case <-timer.C:
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = consumer.Stop(stopCtx)
		return nil, fmt.Errorf("message event consumer did not become ready within %s; stderr=%s", opts.ReadyTimeout, diagnostics.String())
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = consumer.Stop(stopCtx)
		return nil, fmt.Errorf("start message event consumer: %w", ctx.Err())
	}
}

// Stop requests graceful EOF shutdown first. If lark-cli does not honor stdin
// close before the caller's deadline, SIGTERM is used; SIGKILL is never used so
// server-side event subscriptions cannot leak.
func (c *MessageEventConsumer) Stop(ctx context.Context) error {
	if c == nil {
		return nil
	}
	c.stopping.Store(true)
	c.stopOnce.Do(func() { _ = c.stdin.Close() })
	softTimer := time.NewTimer(3 * time.Second)
	defer softTimer.Stop()
	select {
	case <-c.done:
		return c.Err()
	case <-softTimer.C:
		c.termOnce.Do(func() {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Signal(syscall.SIGTERM)
			}
		})
	case <-ctx.Done():
		c.termOnce.Do(func() {
			if c.cmd.Process != nil {
				_ = c.cmd.Process.Signal(syscall.SIGTERM)
			}
		})
		return fmt.Errorf("stop message event consumer: %w", ctx.Err())
	}
	select {
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		return fmt.Errorf("wait for message event consumer after SIGTERM: %w", ctx.Err())
	}
}

func (c *MessageEventConsumer) Done() <-chan struct{} {
	if c == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return c.done
}

func (c *MessageEventConsumer) Err() error {
	if c == nil {
		return nil
	}
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.err
}

type diagnosticBuffer struct {
	mu    sync.Mutex
	lines []string
}

func (b *diagnosticBuffer) Add(line string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lines = append(b.lines, line)
}

func (b *diagnosticBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.lines, "\n")
}
