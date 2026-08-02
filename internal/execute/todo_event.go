package execute

import (
	"encoding/json"
	"fmt"

	"jarvis/internal/domain"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

func createTodoEvent(db *gorm.DB, todoID uint64, fromStatus, toStatus, actor string, detail map[string]any) error {
	encoded, err := json.Marshal(detail)
	if err != nil {
		return fmt.Errorf("encode Todo event detail: %w", err)
	}
	var todo domain.Todo
	if err := db.First(&todo, todoID).Error; err != nil {
		return fmt.Errorf("load Todo id=%d for event snapshot: %w", todoID, err)
	}
	snapshot, err := domain.EncodeTodoEventSnapshot(&todo)
	if err != nil {
		return err
	}
	from := fromStatus
	event := domain.TodoEvent{
		TodoID: todoID, FromStatus: &from, ToStatus: toStatus,
		Actor: actor, Detail: datatypes.JSON(encoded), Snapshot: snapshot,
	}
	if err := db.Create(&event).Error; err != nil {
		return fmt.Errorf("create Todo event todo_id=%d: %w", todoID, err)
	}
	return nil
}
