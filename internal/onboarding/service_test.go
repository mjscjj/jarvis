package onboarding

import (
	"context"
	"strings"
	"testing"
	"time"
)

type streamingRunnerStub struct {
	emitted chan struct{}
	release chan struct{}
}

func (streamingRunnerStub) Run(context.Context, string, []string, string) ([]byte, error) {
	return nil, nil
}

func (runner streamingRunnerStub) RunStreaming(
	_ context.Context,
	_ string,
	_ []string,
	_ string,
	onOutput func([]byte),
) ([]byte, error) {
	output := []byte("Open https://example.test/device and enter ABCD-EFGH\n")
	onOutput(output)
	close(runner.emitted)
	<-runner.release
	return output, nil
}

func TestAgentLoginPublishesDeviceFlowBeforeCommandCompletes(t *testing.T) {
	runner := streamingRunnerStub{
		emitted: make(chan struct{}),
		release: make(chan struct{}),
	}
	service := &Service{
		options: Options{AgentCLIBin: "traex"},
		runner:  runner,
		flows: map[string]*Flow{
			"flow-1": {ID: "flow-1", Status: flowPending},
		},
	}

	done := make(chan struct{})
	go func() {
		service.completeAgentLogin("flow-1")
		close(done)
	}()

	select {
	case <-runner.emitted:
	case <-time.After(time.Second):
		t.Fatal("device flow output was not published")
	}
	flow, err := service.Flow("flow-1")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowPending {
		t.Fatalf("status = %q, want %q", flow.Status, flowPending)
	}
	if !strings.Contains(flow.Output, "ABCD-EFGH") {
		t.Fatalf("pending output = %q, want device code", flow.Output)
	}

	close(runner.release)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("agent login did not finish")
	}
	flow, err = service.Flow("flow-1")
	if err != nil {
		t.Fatal(err)
	}
	if flow.Status != flowSuccess {
		t.Fatalf("status = %q, want %q", flow.Status, flowSuccess)
	}
}
