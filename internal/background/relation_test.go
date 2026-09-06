package background

import (
	"context"
	"encoding/json"
	"testing"

	"jarvis/internal/datatypes"
	"jarvis/internal/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRelationServiceUpsertsCrossModuleEdge(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.EntityRelation{}); err != nil {
		t.Fatal(err)
	}
	service, err := NewRelationService(db)
	if err != nil {
		t.Fatal(err)
	}
	confidence := 0.8
	input := RelationInput{
		SourceType: "okr_kr", SourceID: "kr-1", RelationType: "delivered_by",
		TargetType: "project", TargetID: "42", Evidence: datatypes.JSON(`{"source":"okr-module"}`),
		Confidence: &confidence,
	}
	first, err := service.Upsert(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	confidence = 1
	input.Confidence = &confidence
	second, err := service.Upsert(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID != second.ID || second.Confidence == nil || *second.Confidence != 1 {
		t.Fatalf("upsert created duplicate or lost confidence: first=%+v second=%+v", first, second)
	}
	rows, err := service.List(context.Background(), RelationFilter{SourceType: "okr_kr", SourceID: "kr-1", Limit: 10})
	if err != nil || len(rows) != 1 {
		t.Fatalf("List() = %+v, %v", rows, err)
	}
}

func TestEntityRelationUsesStableSnakeCaseJSON(t *testing.T) {
	raw, err := json.Marshal(domain.EntityRelation{
		ID: 7, SourceType: "okr_kr", SourceID: "kr-1", RelationType: "drives", TargetType: "project", TargetID: "3",
	})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"id", "source_type", "source_id", "relation_type", "target_type", "target_id"} {
		if _, ok := decoded[key]; !ok {
			t.Fatalf("JSON is missing %q: %s", key, raw)
		}
	}
	if _, legacy := decoded["SourceType"]; legacy {
		t.Fatalf("JSON leaked Go field names: %s", raw)
	}
}
