package background

import (
	"testing"
)

func TestProjectInputRejectsUnknownControlValues(t *testing.T) {
	t.Parallel()
	in := ProjectInput{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	if err := in.validate(); err != nil {
		t.Fatalf("valid input rejected: %v", err)
	}
	in.Role = "boss"
	if err := in.validate(); err == nil {
		t.Fatal("invalid role accepted")
	}
}
