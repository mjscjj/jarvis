package decide

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestActionHashCanonicalizesObjects(t *testing.T) {
	first, err := ActionHash("reply", "greet the team", json.RawMessage(`{"steps":["draft"],"mode":"manual"}`))
	if err != nil {
		t.Fatalf("first ActionHash: %v", err)
	}
	second, err := ActionHash("reply", "greet the team", json.RawMessage(`{"mode":"manual","steps":["draft"]}`))
	if err != nil {
		t.Fatalf("second ActionHash: %v", err)
	}
	if first != second {
		t.Fatalf("canonical hashes differ: %q != %q", first, second)
	}

	changed, err := ActionHash("reply", "greet the team", json.RawMessage(`{"mode":"manual","steps":["send"]}`))
	if err != nil {
		t.Fatalf("changed ActionHash: %v", err)
	}
	if changed == first {
		t.Fatal("hash did not change when plan changed")
	}

	retargeted, err := ActionHash("reply", "greet another team", json.RawMessage(`{"steps":["draft"],"mode":"manual"}`))
	if err != nil {
		t.Fatalf("retargeted ActionHash: %v", err)
	}
	if retargeted == first {
		t.Fatal("hash did not change when target changed")
	}
}

func TestActionHashRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name       string
		actionType string
		target     string
		plan       json.RawMessage
	}{
		{name: "blank action", target: "x", plan: json.RawMessage(`{"b":2}`)},
		{name: "blank target", actionType: "reply", target: "  ", plan: json.RawMessage(`{"b":2}`)},
		{name: "array plan", actionType: "reply", target: "x", plan: json.RawMessage(`[]`)},
		{name: "trailing plan", actionType: "reply", target: "x", plan: json.RawMessage(`{"b":2} {}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ActionHash(test.actionType, test.target, test.plan)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}
