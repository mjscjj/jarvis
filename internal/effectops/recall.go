// Package effectops owns user-triggered operations on external effects that an
// execution run already declared. It is intentionally separate from the M5
// execution lifecycle.
package effectops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"jarvis/internal/domain"
	"jarvis/internal/progress"

	"gorm.io/gorm"
	"jarvis/internal/datatypes"
)

var (
	ErrInvalidInput    = errors.New("invalid effect operation input")
	ErrTaskNotFound    = errors.New("effect operation Task not found")
	ErrVersionConflict = errors.New("effect operation version conflict")
	// ErrRecallTargetNotFound means the message id was never declared as an
	// effect of this Task, so Jarvis refuses to recall it.
	ErrRecallTargetNotFound = errors.New("recall target message is not an effect of this Task")
	// ErrMessageAlreadyRecalled means the effect already carries a recall mark.
	ErrMessageAlreadyRecalled = errors.New("feishu message is already recalled")
)

// MessageRecallClient is the process boundary that performs the real recall.
// In production it is *larkcli.Client.
type MessageRecallClient interface {
	RecallMessage(ctx context.Context, messageID string) error
}

// MessageRecaller recalls one Feishu message Jarvis already sent and writes the
// "已撤回" mark back onto the effect that declared it.
//
// Effects stay an open, agent-declared payload: the mark is a single extra
// recalled_at key added to the matching effect objects, and no field the agent
// declared is rewritten or dropped. The same message is usually declared twice
// (task.execution_result plus the run that sent it), so both copies are marked
// to keep the UI consistent wherever the effect is rendered.
type MessageRecaller struct {
	db   *gorm.DB
	lark MessageRecallClient
	now  func() time.Time
}

func NewMessageRecaller(db *gorm.DB, lark MessageRecallClient) (*MessageRecaller, error) {
	if db == nil {
		return nil, fmt.Errorf("message recaller db is nil")
	}
	if lark == nil {
		return nil, fmt.Errorf("message recaller lark client is nil")
	}
	return &MessageRecaller{db: db, lark: lark, now: time.Now}, nil
}

// Recall recalls messageID and persists the audit mark.
//
// The message must be declared as an effect of this Task: that declaration is
// the only proof Jarvis sent it, and requiring it keeps this endpoint from
// deleting arbitrary Feishu messages. Recalling is irreversible, so it happens
// after the scan and before any write; if persisting the mark then fails the
// error surfaces as-is (no rollback, no retry — the message is already gone).
func (r *MessageRecaller) Recall(ctx context.Context, taskID uint64, messageID string) error {
	if taskID == 0 {
		return fmt.Errorf("%w: Task ID is invalid", ErrInvalidInput)
	}
	messageID = strings.TrimSpace(messageID)
	if !strings.HasPrefix(messageID, "om_") {
		return fmt.Errorf("%w: message_id must be a Feishu message id (om_...)", ErrInvalidInput)
	}

	var task domain.Task
	if err := r.db.WithContext(ctx).First(&task, taskID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: task_id=%d", ErrTaskNotFound, taskID)
		}
		return fmt.Errorf("load Task id=%d for recall: %w", taskID, err)
	}
	var runs []domain.ExecutionRun
	if err := r.db.WithContext(ctx).Where("task_id = ?", taskID).Order("id").Find(&runs).Error; err != nil {
		return fmt.Errorf("load runs of Task id=%d for recall: %w", taskID, err)
	}

	recalledAt := r.now().UTC().Format(time.RFC3339)
	taskScan, err := markRecalledInExecutionResult(task.ExecutionResult, messageID, recalledAt)
	if err != nil {
		return fmt.Errorf("scan execution_result effects task_id=%d: %w", taskID, err)
	}
	matched, alreadyRecalled := taskScan.matched, taskScan.alreadyRecalled
	runPatches := make(map[uint64][]byte)
	for i := range runs {
		scan, err := markRecalledInEffects(runs[i].Effects, messageID, recalledAt)
		if err != nil {
			return fmt.Errorf("scan run effects run_id=%d: %w", runs[i].ID, err)
		}
		matched += scan.matched
		alreadyRecalled += scan.alreadyRecalled
		if scan.patched != nil {
			runPatches[runs[i].ID] = scan.patched
		}
	}
	if matched == 0 {
		return fmt.Errorf("%w: task_id=%d message_id=%s", ErrRecallTargetNotFound, taskID, messageID)
	}
	if alreadyRecalled > 0 {
		return fmt.Errorf("%w: task_id=%d message_id=%s", ErrMessageAlreadyRecalled, taskID, messageID)
	}

	if err := r.lark.RecallMessage(ctx, messageID); err != nil {
		return fmt.Errorf("recall feishu message task_id=%d message_id=%s: %w", taskID, messageID, err)
	}

	for runID, patched := range runPatches {
		update := r.db.WithContext(ctx).Model(&domain.ExecutionRun{}).
			Where("id = ? AND task_id = ?", runID, taskID).
			Update("effects", datatypes.JSON(patched))
		if update.Error != nil {
			return fmt.Errorf("mark recalled effect run_id=%d: %w", runID, update.Error)
		}
		if update.RowsAffected != 1 {
			return fmt.Errorf("mark recalled effect run_id=%d: run is gone", runID)
		}
	}

	// The Task version is bumped even when the mark only landed on old runs:
	// task_event is unique per (task_id, task_version), so the audit event needs
	// a fresh version. The read version is the CAS guard against a concurrent
	// M5 write overwriting execution_result.
	updates := map[string]any{"version": gorm.Expr("version + 1")}
	if taskScan.patched != nil {
		updates["execution_result"] = datatypes.JSON(taskScan.patched)
	}
	update := r.db.WithContext(ctx).Model(&domain.Task{}).
		Where("id = ? AND version = ?", task.ID, task.Version).Updates(updates)
	if update.Error != nil {
		return fmt.Errorf("mark recalled effect task_id=%d: %w", task.ID, update.Error)
	}
	if update.RowsAffected != 1 {
		return fmt.Errorf("%w: task_id=%d expected=%d", ErrVersionConflict, task.ID, task.Version)
	}
	if err := progress.AppendTaskEvent(r.db.WithContext(ctx), progress.TaskEventInput{
		TaskID: task.ID, TaskVersion: task.Version + 1, EventType: "feishu_message_recalled",
		FromStatus: &task.Status, ToStatus: task.Status, ActorType: "user",
		Detail:     map[string]any{"message_id": messageID, "effects_marked": matched},
		OccurredAt: r.now().UTC(),
	}); err != nil {
		return err
	}
	return nil
}

// effectScan is the outcome of looking for one message id in one effect payload:
// how many effects declared it, how many already carry a recall mark, and the
// re-encoded payload with the mark applied (nil when nothing matched).
type effectScan struct {
	matched         int
	alreadyRecalled int
	patched         []byte
}

// markRecalledInEffects works on an effects array (execution_run.effects).
func markRecalledInEffects(raw []byte, messageID, recalledAt string) (effectScan, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return effectScan{}, nil
	}
	var effects []any
	if err := decodeJSONPreservingNumbers(raw, &effects); err != nil {
		return effectScan{}, fmt.Errorf("decode effects array: %w", err)
	}
	var scan effectScan
	for _, item := range effects {
		effect, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if stringField(effect, "message_id") != messageID {
			continue
		}
		scan.matched++
		if stringField(effect, "recalled_at") != "" {
			scan.alreadyRecalled++
			continue
		}
		effect["recalled_at"] = recalledAt
	}
	if scan.matched == 0 {
		return scan, nil
	}
	encoded, err := json.Marshal(effects)
	if err != nil {
		return effectScan{}, fmt.Errorf("encode marked effects: %w", err)
	}
	scan.patched = encoded
	return scan, nil
}

// markRecalledInExecutionResult works on a Task's execution_result object, whose
// "effects" key holds the same array. Every other key travels untouched.
func markRecalledInExecutionResult(raw []byte, messageID, recalledAt string) (effectScan, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return effectScan{}, nil
	}
	var result map[string]any
	if err := decodeJSONPreservingNumbers(raw, &result); err != nil {
		return effectScan{}, fmt.Errorf("decode execution_result: %w", err)
	}
	declared, ok := result["effects"]
	if !ok {
		return effectScan{}, nil
	}
	encodedEffects, err := json.Marshal(declared)
	if err != nil {
		return effectScan{}, fmt.Errorf("encode declared effects: %w", err)
	}
	scan, err := markRecalledInEffects(encodedEffects, messageID, recalledAt)
	if err != nil || scan.patched == nil {
		return scan, err
	}
	var marked any
	if err := decodeJSONPreservingNumbers(scan.patched, &marked); err != nil {
		return effectScan{}, fmt.Errorf("decode marked effects: %w", err)
	}
	result["effects"] = marked
	encoded, err := json.Marshal(result)
	if err != nil {
		return effectScan{}, fmt.Errorf("encode marked execution_result: %w", err)
	}
	scan.patched = encoded
	return scan, nil
}

func decodeJSONPreservingNumbers(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	return decoder.Decode(target)
}

func stringField(object map[string]any, key string) string {
	value, ok := object[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}
