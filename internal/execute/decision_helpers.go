package execute

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"jarvis/internal/contextsnap"
	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// requireContextSnapshot returns the Todo's M3-frozen context_snapshot as raw
// JSON, failing fast if it is missing or malformed. There is deliberately no
// re-snapshot fallback: an empty snapshot is a real bug that must surface.
func requireContextSnapshot(todo *domain.Todo) (json.RawMessage, error) {
	raw := []byte(todo.ContextSnapshot)
	if _, err := contextsnap.Decode(raw); err != nil {
		return nil, fmt.Errorf("%w: todo_id=%d context_snapshot invalid: %v", ErrInvalidInput, todo.ID, err)
	}
	return json.RawMessage(append([]byte(nil), raw...)), nil
}

func lockTodo(tx *gorm.DB, todoID uint64, todo *domain.Todo) error {
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(todo, todoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w: todo_id=%d", ErrTodoNotFound, todoID)
	}
	if err != nil {
		return fmt.Errorf("lock decision Todo id=%d: %w", todoID, err)
	}
	return nil
}

func createTodoEvent(tx *gorm.DB, todoID uint64, fromStatus, toStatus string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode decision Todo event detail: %w", err)
	}
	snapshot, err := loadTodoEventSnapshot(tx, todoID)
	if err != nil {
		return err
	}
	from := fromStatus
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: toStatus,
		Actor: "m5", Detail: datatypes.JSON(encoded), Snapshot: snapshot,
	}
	if err := tx.Create(&event).Error; err != nil {
		return fmt.Errorf("create decision Todo event todo_id=%d: %w", todoID, err)
	}
	return nil
}

func loadTodoEventSnapshot(db *gorm.DB, todoID uint64) (datatypes.JSON, error) {
	var todo domain.Todo
	if err := db.First(&todo, todoID).Error; err != nil {
		return nil, fmt.Errorf("load todo id=%d for event snapshot: %w", todoID, err)
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&todo)
	if err != nil {
		return nil, err
	}
	return snapshot, nil
}

// canonicalNamedJSONObject validates one named JSON object and re-encodes it in
// canonical form. The name only enriches error messages; store.go's
// canonicalJSONObject covers the unnamed Task-side case.
func canonicalNamedJSONObject(raw []byte, name string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrInvalidInput, name, err)
	}
	if len(object) == 0 {
		return nil, fmt.Errorf("%w: %s must be a non-empty JSON object", ErrInvalidInput, name)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %s contains multiple JSON values", ErrInvalidInput, name)
		}
		return nil, fmt.Errorf("%w: decode trailing %s: %v", ErrInvalidInput, name, err)
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("encode canonical %s: %w", name, err)
	}
	return json.RawMessage(encoded), nil
}

// canonicalJSONValue validates and canonicalizes one open semantic JSON value.
// It intentionally does not prescribe object fields. When allowEmpty is false,
// null, blank strings, empty arrays and empty objects are rejected.
func canonicalJSONValue(raw []byte, name string, allowEmpty bool) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("%w: %s is required", ErrInvalidInput, name)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, fmt.Errorf("%w: decode %s: %v", ErrInvalidInput, name, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("%w: %s contains multiple JSON values", ErrInvalidInput, name)
		}
		return nil, fmt.Errorf("%w: decode trailing %s: %v", ErrInvalidInput, name, err)
	}
	if value == nil {
		return nil, fmt.Errorf("%w: %s must not be null", ErrInvalidInput, name)
	}
	if !allowEmpty {
		switch typed := value.(type) {
		case string:
			if strings.TrimSpace(typed) == "" {
				return nil, fmt.Errorf("%w: %s must not be blank", ErrInvalidInput, name)
			}
		case []any:
			if len(typed) == 0 {
				return nil, fmt.Errorf("%w: %s must not be an empty array", ErrInvalidInput, name)
			}
		case map[string]any:
			if len(typed) == 0 {
				return nil, fmt.Errorf("%w: %s must not be an empty object", ErrInvalidInput, name)
			}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode canonical %s: %w", name, err)
	}
	return json.RawMessage(encoded), nil
}

func versionConflict(todoID uint64, expected, actual int32) error {
	return fmt.Errorf("%w: todo_id=%d expected=%d actual=%d", ErrVersionConflict, todoID, expected, actual)
}

func transitionError(todoID uint64, from, to string) error {
	return fmt.Errorf("%w: todo_id=%d from=%s to=%s", ErrInvalidTransition, todoID, from, to)
}

func copyUint64(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
