// Package semantic owns the dedicated Qdrant collection used for Todo
// semantic deduplication. SQLite remains the source of truth.
package semantic

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/qdrant/go-client/qdrant"
)

type Options struct {
	Host           string
	Port           int
	Collection     string
	EmbeddingModel string
	Dimensions     int
	ScoreThreshold float64
	NeighborLimit  int
	ActiveStatuses []string
}

type Match struct {
	TodoID      uint64
	Fingerprint string
	Score       float32
}

type Record struct {
	TodoID      uint64
	Fingerprint string
	ProjectID   *uint64
	Status      string
	ActionType  string
	Vector      []float32
}

type Index struct {
	client         *qdrant.Client
	collection     string
	embeddingModel string
	dimensions     int
	scoreThreshold float32
	neighborLimit  uint64
	activeStatuses []string
}

func NewIndex(opts Options) (*Index, error) {
	if strings.TrimSpace(opts.Host) == "" {
		return nil, fmt.Errorf("semantic qdrant host is empty")
	}
	if opts.Port <= 0 || opts.Port > 65535 {
		return nil, fmt.Errorf("semantic qdrant port must be between 1 and 65535")
	}
	if strings.TrimSpace(opts.Collection) == "" {
		return nil, fmt.Errorf("semantic qdrant collection is empty")
	}
	if strings.TrimSpace(opts.EmbeddingModel) == "" {
		return nil, fmt.Errorf("semantic embedding model is empty")
	}
	if opts.Dimensions <= 0 {
		return nil, fmt.Errorf("semantic embedding dimensions must be positive")
	}
	if opts.ScoreThreshold <= 0 || opts.ScoreThreshold > 1 {
		return nil, fmt.Errorf("semantic score threshold must be in (0,1]")
	}
	if opts.NeighborLimit <= 0 {
		return nil, fmt.Errorf("semantic neighbor limit must be positive")
	}
	if len(opts.ActiveStatuses) == 0 {
		return nil, fmt.Errorf("semantic active statuses must not be empty")
	}
	statuses := make([]string, len(opts.ActiveStatuses))
	for i, status := range opts.ActiveStatuses {
		status = strings.TrimSpace(status)
		if status == "" {
			return nil, fmt.Errorf("semantic active status[%d] is empty", i)
		}
		statuses[i] = status
	}
	client, err := qdrant.NewClient(&qdrant.Config{
		Host: opts.Host, Port: opts.Port, PoolSize: 1, SkipCompatibilityCheck: true,
	})
	if err != nil {
		return nil, fmt.Errorf("initialize semantic qdrant client: %w", err)
	}
	return &Index{
		client: client, collection: opts.Collection, embeddingModel: opts.EmbeddingModel, dimensions: opts.Dimensions,
		scoreThreshold: float32(opts.ScoreThreshold), neighborLimit: uint64(opts.NeighborLimit),
		activeStatuses: statuses,
	}, nil
}

func (i *Index) Close() error {
	if i == nil || i.client == nil {
		return nil
	}
	return i.client.Close()
}

// HealthCheck reports whether Qdrant answers, and which version answered. It
// does not touch the collection: Ensure already established that at startup, and
// a readiness probe must not create or migrate anything.
func (i *Index) HealthCheck(ctx context.Context) (string, error) {
	if i == nil || i.client == nil {
		return "", fmt.Errorf("semantic index is not initialized")
	}
	reply, err := i.client.HealthCheck(ctx)
	if err != nil {
		return "", fmt.Errorf("qdrant health check: %w", err)
	}
	return reply.GetVersion(), nil
}

func (i *Index) Ensure(ctx context.Context) error {
	exists, err := i.client.CollectionExists(ctx, i.collection)
	if err != nil {
		return fmt.Errorf("check semantic collection %q: %w", i.collection, err)
	}
	if !exists {
		metadata, err := qdrant.TryValueMap(map[string]any{"embedding_model": i.embeddingModel})
		if err != nil {
			return fmt.Errorf("encode semantic collection metadata: %w", err)
		}
		if err := i.client.CreateCollection(ctx, &qdrant.CreateCollection{
			CollectionName: i.collection,
			VectorsConfig: qdrant.NewVectorsConfig(&qdrant.VectorParams{
				Size: uint64(i.dimensions), Distance: qdrant.Distance_Cosine,
			}), Metadata: metadata,
		}); err != nil {
			return fmt.Errorf("create semantic collection %q: %w", i.collection, err)
		}
		return nil
	}
	info, err := i.client.GetCollectionInfo(ctx, i.collection)
	if err != nil {
		return fmt.Errorf("inspect semantic collection %q: %w", i.collection, err)
	}
	params := info.GetConfig().GetParams().GetVectorsConfig().GetParams()
	if params == nil {
		return fmt.Errorf("semantic collection %q must use one unnamed dense vector", i.collection)
	}
	if params.GetSize() != uint64(i.dimensions) || params.GetDistance() != qdrant.Distance_Cosine {
		return fmt.Errorf(
			"semantic collection %q vector config size=%d distance=%s, want size=%d distance=Cosine",
			i.collection, params.GetSize(), params.GetDistance().String(), i.dimensions,
		)
	}
	model := info.GetConfig().GetMetadata()["embedding_model"].GetStringValue()
	if model != i.embeddingModel {
		return fmt.Errorf("semantic collection %q embedding_model=%q, want %q", i.collection, model, i.embeddingModel)
	}
	if err := migrateLegacyTodoStatuses(ctx, i.client, i.collection); err != nil {
		return err
	}
	return nil
}

func (i *Index) Search(ctx context.Context, vector []float32, projectID *uint64, actionType string) ([]Match, error) {
	if err := i.validateVector(vector); err != nil {
		return nil, err
	}
	if strings.TrimSpace(actionType) == "" {
		return nil, fmt.Errorf("semantic action type is empty")
	}
	points, err := i.client.Query(ctx, &qdrant.QueryPoints{
		CollectionName: i.collection,
		Query:          qdrant.NewQueryDense(vector),
		Filter: &qdrant.Filter{Must: []*qdrant.Condition{
			qdrant.NewMatchKeyword("project_id", projectScope(projectID)),
			qdrant.NewMatchKeywords("status", i.activeStatuses...),
			qdrant.NewMatchKeyword("action_type", actionType),
		}},
		ScoreThreshold: qdrant.PtrOf(i.scoreThreshold),
		Limit:          qdrant.PtrOf(i.neighborLimit),
		WithPayload:    qdrant.NewWithPayloadInclude("fingerprint"),
	})
	if err != nil {
		return nil, fmt.Errorf("query semantic collection %q: %w", i.collection, err)
	}
	matches := make([]Match, 0, len(points))
	for position, point := range points {
		todoID := point.GetId().GetNum()
		if todoID == 0 {
			return nil, fmt.Errorf("semantic result[%d] has non-numeric or zero Todo ID", position)
		}
		fingerprint := point.GetPayload()["fingerprint"].GetStringValue()
		if strings.TrimSpace(fingerprint) == "" {
			return nil, fmt.Errorf("semantic result todo_id=%d has empty fingerprint payload", todoID)
		}
		matches = append(matches, Match{TodoID: todoID, Fingerprint: fingerprint, Score: point.GetScore()})
	}
	return matches, nil
}

func (i *Index) Upsert(ctx context.Context, records []Record) error {
	if len(records) == 0 {
		return nil
	}
	points := make([]*qdrant.PointStruct, len(records))
	seen := make(map[uint64]struct{}, len(records))
	for position, record := range records {
		if record.TodoID == 0 || strings.TrimSpace(record.Fingerprint) == "" || strings.TrimSpace(record.Status) == "" || strings.TrimSpace(record.ActionType) == "" {
			return fmt.Errorf("semantic record[%d] has invalid Todo identity", position)
		}
		if _, exists := seen[record.TodoID]; exists {
			return fmt.Errorf("semantic records contain duplicate todo_id=%d", record.TodoID)
		}
		seen[record.TodoID] = struct{}{}
		if err := i.validateVector(record.Vector); err != nil {
			return fmt.Errorf("semantic record todo_id=%d: %w", record.TodoID, err)
		}
		payload, err := qdrant.TryValueMap(map[string]any{
			"fingerprint": record.Fingerprint,
			"project_id":  projectScope(record.ProjectID),
			"status":      record.Status,
			"action_type": record.ActionType,
		})
		if err != nil {
			return fmt.Errorf("encode semantic record todo_id=%d payload: %w", record.TodoID, err)
		}
		points[position] = &qdrant.PointStruct{
			Id: qdrant.NewIDNum(record.TodoID), Vectors: qdrant.NewVectorsDense(record.Vector),
			Payload: payload,
		}
	}
	wait := true
	if _, err := i.client.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: i.collection, Wait: &wait, Points: points,
	}); err != nil {
		return fmt.Errorf("upsert %d Todo vectors into semantic collection %q: %w", len(points), i.collection, err)
	}
	return nil
}

func (i *Index) validateVector(vector []float32) error {
	if len(vector) != i.dimensions {
		return fmt.Errorf("semantic vector dimensions=%d, want %d", len(vector), i.dimensions)
	}
	for position, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("semantic vector value[%d] is not finite", position)
		}
	}
	return nil
}

func projectScope(projectID *uint64) string {
	if projectID == nil {
		return "unassigned"
	}
	return strconv.FormatUint(*projectID, 10)
}
