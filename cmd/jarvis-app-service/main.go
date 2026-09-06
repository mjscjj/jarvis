// Command jarvis-app-service owns the desktop application's local Go runtime.
// A thin macOS shell launches it as a child and consumes the single
// JARVIS_RUNTIME_CONNECTION line written after all required services are ready.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"jarvis/internal/appservice"
)

func main() {
	resourceRoot := flag.String("resources", "", "bundled runtime resource root")
	stateRoot := flag.String("state", "", "writable application state root")
	address := flag.String("address", "127.0.0.1:18800", "Jarvis HTTP address announced to the desktop shell")
	supervisorPID := flag.Int("supervisor-pid", 0, "desktop shell pid; exiting it stops this service")
	startupTimeout := flag.Duration("startup-timeout", 30*time.Second, "maximum wait for Qdrant and Jarvis readiness")
	flag.Parse()

	service, err := appservice.New(appservice.Options{
		ResourceRoot:   *resourceRoot,
		StateRoot:      *stateRoot,
		Address:        *address,
		SupervisorPID:  *supervisorPID,
		StartupTimeout: *startupTimeout,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "jarvis-app-service: %v\n", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	if err := service.Run(ctx, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "jarvis-app-service: %v\n", err)
		os.Exit(1)
	}
}
