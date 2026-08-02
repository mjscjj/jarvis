package background

import (
	"encoding/json"
	"reflect"
	"testing"

	"jarvis/internal/domain"

	"jarvis/internal/datatypes"
)

func TestProjectChangedFields(t *testing.T) {
	t.Parallel()
	description := "old"
	notes := "notes"
	before := &domain.Project{
		Name: "Jarvis", Role: "owner", Status: "planning", Priority: 2,
		Description: &description, Repos: datatypes.JSON(`[{"path":"/repo"}]`), Notes: &notes,
	}
	updatedDescription := "new"
	input := ProjectInput{
		Name: "Jarvis", Role: "owner", Status: "active", Priority: 2,
		Description: &updatedDescription, Repos: json.RawMessage(`[{"path":"/repo"}]`), Notes: &notes,
	}
	if got, want := projectChangedFields(before, input), []string{"status", "description"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("changed fields = %#v, want %#v", got, want)
	}
}

func TestProjectChangedFieldsNoChange(t *testing.T) {
	t.Parallel()
	before := &domain.Project{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	input := ProjectInput{Name: "Jarvis", Role: "owner", Status: "active", Priority: 1}
	if got := projectChangedFields(before, input); len(got) != 0 {
		t.Fatalf("changed fields = %#v, want empty", got)
	}
}
