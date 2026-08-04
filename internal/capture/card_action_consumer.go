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

const cardActionEventReadyMarker = "[event] ready event_key=" + cardActionEventType

// CardActionHandler lands one authorized card.action.trigger event. It is
// injected so the approve/reject business wiring lives with the executor and
// this package keeps owning only the lark-cli subprocess boundary.
type CardActionHandler interface {
	HandleCardAction(context.Context, CardActionEvent) error
}

type CardActionConsumerOptions struct {
	Bin          string
	Profile      string
	ReadyTimeout time.Duration
}

// CardActionConsumer owns the one unbounded lark-cli event process for card
// callbacks. It is a deliberate copy of MessageEventConsumer's subprocess
// contract (ready-marker gate, stdin-EOF graceful shutdown, never kill -9) for a
// second EventKey; the two share one lark-cli bus daemon, so running both is
// safe. stdin stays open because lark-cli treats EOF as a shutdown request.
type CardActionConsumer struct {
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

// StartCardActionConsumer blocks until lark-cli emits its documented ready
// marker so a failed subscription surfaces synchronously instead of silently
// dropping every button click.
func StartCardActionConsumer(
	ctx context.Context,
	handler CardActionHandler,
	opts CardActionConsumerOptions,
	logger *log.Logger,
) (*CardActionConsumer, error) {
	if handler == nil {
		return nil, fmt.Errorf("card action handler is nil")
	}
	if strings.TrimSpace(opts.Bin) == "" {
		return nil, fmt.Errorf("card action lark-cli bin is empty")
	}
	if strings.TrimSpace(opts.Profile) == "" {
		return nil, fmt.Errorf("card action lark-cli profile is empty")
	}
	if opts.ReadyTimeout <= 0 {
		return nil, fmt.Errorf("card action ready timeout must be positive")
	}
	if logger == nil {
		return nil, fmt.Errorf("card action logger is nil")
	}

	bin, err := exec.LookPath(opts.Bin)
	if err != nil {
		return nil, fmt.Errorf("resolve card action lark-cli binary %q: %w", opts.Bin, err)
	}
	cmd := exec.Command(
		bin,
		"--profile", opts.Profile,
		"event", "consume", cardActionEventType,
		"--as", "bot",
	)
	cmd.Env = append(os.Environ(),
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open card action stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open card action stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("open card action stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, fmt.Errorf("start card action consumer: %w", err)
	}

	consumer := &CardActionConsumer{cmd: cmd, stdin: stdin, done: make(chan struct{})}
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
			if strings.HasPrefix(line, cardActionEventReadyMarker) {
				consumer.ready.Store(true)
				readyOnce.Do(func() { close(ready) })
				continue
			}
			logger.Printf("job=card-action status=info diagnostic=%q", line)
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
					logger.Printf("job=card-action status=error error=decode event stream: %+v", err)
					consumer.stopOnce.Do(func() { _ = consumer.stdin.Close() })
				}
				return
			}
			var event CardActionEvent
			if err := json.Unmarshal(raw, &event); err != nil {
				logger.Printf("job=card-action status=error error=decode card action event: %+v raw=%s", err, raw)
				continue
			}
			if err := validateCardActionEvent(event); err != nil {
				logger.Printf("job=card-action status=error error=%+v raw=%s", err, raw)
				continue
			}
			if err := handler.HandleCardAction(ctx, event); err != nil {
				logger.Printf("job=card-action status=error error=handle card action: %+v event_id=%s", err, event.EventID)
				continue
			}
			logger.Printf("job=card-action status=ok event_id=%s operator=%s message_id=%s", event.EventID, event.OperatorID, event.MessageID)
		}
	}()

	go func() {
		waitErr := cmd.Wait()
		readers.Wait()
		if !consumer.stopping.Load() && ctx.Err() == nil {
			if waitErr == nil {
				waitErr = fmt.Errorf("card action consumer exited unexpectedly")
			} else {
				waitErr = fmt.Errorf("card action consumer exited: %w", waitErr)
			}
			if detail := diagnostics.String(); detail != "" {
				waitErr = fmt.Errorf("%w; stderr=%s", waitErr, detail)
			}
			logger.Printf("job=card-action status=error error=%+v", waitErr)
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
				logger.Printf("job=card-action status=error error=stop card action consumer: %+v", err)
			}
		case <-consumer.done:
		}
	}()

	timer := time.NewTimer(opts.ReadyTimeout)
	defer timer.Stop()
	select {
	case <-ready:
		logger.Printf("job=card-action status=ok state=ready profile=%s", opts.Profile)
		return consumer, nil
	case <-consumer.done:
		return nil, fmt.Errorf("card action consumer stopped before ready: %w", consumer.Err())
	case <-timer.C:
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = consumer.Stop(stopCtx)
		return nil, fmt.Errorf("card action consumer did not become ready within %s; stderr=%s", opts.ReadyTimeout, diagnostics.String())
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = consumer.Stop(stopCtx)
		return nil, fmt.Errorf("start card action consumer: %w", ctx.Err())
	}
}

// Stop requests graceful EOF shutdown first, then SIGTERM; SIGKILL is never used
// so the server-side event subscription cannot leak.
func (c *CardActionConsumer) Stop(ctx context.Context) error {
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
		return fmt.Errorf("stop card action consumer: %w", ctx.Err())
	}
	select {
	case <-c.done:
		return c.Err()
	case <-ctx.Done():
		return fmt.Errorf("wait for card action consumer after SIGTERM: %w", ctx.Err())
	}
}

func (c *CardActionConsumer) Done() <-chan struct{} {
	if c == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return c.done
}

func (c *CardActionConsumer) Err() error {
	if c == nil {
		return nil
	}
	c.errMu.RLock()
	defer c.errMu.RUnlock()
	return c.err
}
