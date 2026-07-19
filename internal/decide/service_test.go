package decide

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestActionHashCanonicalizesObjects(t *testing.T) {
	first, err := ActionHash("reply", json.RawMessage(`{"b":2,"a":1}`), json.RawMessage(`{"steps":["draft"],"mode":"manual"}`))
	if err != nil {
		t.Fatalf("first ActionHash: %v", err)
	}
	second, err := ActionHash("reply", json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"mode":"manual","steps":["draft"]}`))
	if err != nil {
		t.Fatalf("second ActionHash: %v", err)
	}
	if first != second {
		t.Fatalf("canonical hashes differ: %q != %q", first, second)
	}

	changed, err := ActionHash("reply", json.RawMessage(`{"a":1,"b":2}`), json.RawMessage(`{"mode":"manual","steps":["send"]}`))
	if err != nil {
		t.Fatalf("changed ActionHash: %v", err)
	}
	if changed == first {
		t.Fatal("hash did not change when plan changed")
	}
}

func TestActionHashRejectsInvalidInput(t *testing.T) {
	tests := []struct {
		name       string
		actionType string
		slots      json.RawMessage
		plan       json.RawMessage
	}{
		{name: "blank action", slots: json.RawMessage(`{"a":1}`), plan: json.RawMessage(`{"b":2}`)},
		{name: "empty slots", actionType: "reply", slots: json.RawMessage(`{}`), plan: json.RawMessage(`{"b":2}`)},
		{name: "array plan", actionType: "reply", slots: json.RawMessage(`{"a":1}`), plan: json.RawMessage(`[]`)},
		{name: "trailing plan", actionType: "reply", slots: json.RawMessage(`{"a":1}`), plan: json.RawMessage(`{"b":2} {}`)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ActionHash(test.actionType, test.slots, test.plan)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}
