package execute

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
)

// SaveObservations stores execution-time observations idempotently. Re-running a
// Task legitimately rediscovers the same fact, so a duplicate key is a no-op.
func (s *Store) SaveObservations(ctx context.Context, rows []domain.Observation) (int, error) {
	created := 0
	for i := range rows {
		result := s.db.WithContext(ctx).Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "dedup_key"}},
			DoNothing: true,
		}).Create(&rows[i])
		if result.Error != nil {
			return created, fmt.Errorf("persist execution observation subject=%q: %w", rows[i].Subject, result.Error)
		}
		created += int(result.RowsAffected)
	}
	return created, nil
}

// recordRunObservations stores what this run learned, for every terminal state:
// a run that ended in waiting or failed often learned the most useful things.
//
// This is bookkeeping alongside the real result, so a failure to store it is
// logged rather than propagated — the execution already happened and its
// outcome must still be recorded.
func (e *AgentExecutor) recordRunObservations(ctx context.Context, task *domain.Task, run *domain.ExecutionRun) {
	rows, err := runObservations(task, run, time.Local)
	if err != nil {
		log.Printf("m5 observation build failed task_id=%d run_id=%d: %v", task.ID, run.ID, err)
		return
	}
	if len(rows) == 0 {
		return
	}
	if _, err := e.store.SaveObservations(ctx, rows); err != nil {
		log.Printf("m5 observation save failed task_id=%d run_id=%d: %v", task.ID, run.ID, err)
	}
}

// runObservations turns the run's declared enrichments into observations.
//
// Enrichments are what the agent learned on the way that nobody asked for — a
// build step this repo needs, who actually owns a service, why an approach did
// not work. They were previously kept only inside execution_result for display,
// so the next Task started blind to all of it. Recording them as observations
// puts them in the same place M3's observations live.
func runObservations(task *domain.Task, run *domain.ExecutionRun, location *time.Location) ([]domain.Observation, error) {
	if task == nil || run == nil || len(run.Output) == 0 {
		return nil, nil
	}
	var structured struct {
		Enrichments []struct {
			Kind    string `json:"kind"`
			Label   string `json:"label"`
			Content string `json:"content"`
		} `json:"enrichments"`
	}
	if err := json.Unmarshal(run.Output, &structured); err != nil {
		// The run already happened and its result is stored; a shape we cannot
		// read here must not turn a finished execution into a failure.
		return nil, nil
	}
	rows := make([]domain.Observation, 0, len(structured.Enrichments))
	for _, enrichment := range structured.Enrichments {
		subject := strings.TrimSpace(enrichment.Label)
		content := strings.TrimSpace(enrichment.Content)
		if subject == "" || content == "" {
			continue
		}
		payload, err := json.Marshal(map[string]string{"kind": strings.TrimSpace(enrichment.Kind)})
		if err != nil {
			return nil, fmt.Errorf("encode observation payload task_id=%d: %w", task.ID, err)
		}
		observedAt := run.StartedAt.In(location)
		if run.FinishedAt != nil {
			observedAt = run.FinishedAt.In(location)
		}
		rows = append(rows, domain.Observation{
			Producer:    domain.ObservationProducerM5,
			Subject:     subject,
			Content:     content,
			ProjectID:   copyUint64(task.ProjectID),
			SourceRunID: &run.ID,
			Payload:     datatypes.JSON(payload),
			DedupKey:    executionObservationDedupKey(task, subject, content),
			ObservedAt:  observedAt,
		})
	}
	return rows, nil
}

// executionObservationDedupKey keys on the Task rather than the run: re-running
// the same Task and rediscovering the same fact is the duplicate we want to
// collapse.
func executionObservationDedupKey(task *domain.Task, subject, content string) string {
	payload := strings.Join([]string{
		domain.ObservationProducerM5,
		fmt.Sprintf("%d", task.ID),
		strings.Join(strings.Fields(subject), " "),
		strings.Join(strings.Fields(content), " "),
	}, "\x00")
	hash := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(hash[:])
}
