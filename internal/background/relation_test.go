package background

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
		SourceType: "okr_kr", SourceID: "kr-1", RelationType: "maps_to",
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

func TestRelationServiceFiltersNeighborsTypeAndCursor(t *testing.T) {
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
	for _, input := range []RelationInput{
		{SourceType: "project", SourceID: "7", RelationType: "maps_to", TargetType: "okr_kr", TargetID: "kr-1"},
		{SourceType: "okr_point", SourceID: "point-1", RelationType: "advances", TargetType: "project", TargetID: "7"},
		{SourceType: "project", SourceID: "8", RelationType: "maps_to", TargetType: "okr_kr", TargetID: "kr-2"},
	} {
		if _, err := service.Upsert(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	first, err := service.ListPage(context.Background(), RelationFilter{
		NodeType: "project", NodeID: "7", Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Items) != 1 || first.NextCursor == "" {
		t.Fatalf("first page = %#v", first)
	}
	var cursor uint64
	if _, err := fmt.Sscan(first.NextCursor, &cursor); err != nil {
		t.Fatalf("parse cursor %q: %v", first.NextCursor, err)
	}
	second, err := service.ListPage(context.Background(), RelationFilter{
		NodeType: "project", NodeID: "7", RelationType: "maps_to", Cursor: cursor, Limit: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Items) != 1 || second.Items[0].RelationType != "maps_to" || second.NextCursor != "" {
		t.Fatalf("second page = %#v", second)
	}
}

func TestRelationServiceRejectsPartialNeighborIdentity(t *testing.T) {
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
	if _, err := service.ListPage(context.Background(), RelationFilter{NodeType: "project", Limit: 10}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestRelationServiceFiltersEdgesTouchingAnyNodeType(t *testing.T) {
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
	for _, input := range []RelationInput{
		{SourceType: "okr_kr", SourceID: "kr-1", RelationType: "maps_to", TargetType: "project", TargetID: "7"},
		{SourceType: "person", SourceID: "9", RelationType: "owns", TargetType: "okr_point", TargetID: "point-1"},
		{SourceType: "group", SourceID: "3", RelationType: "belongs_to", TargetType: "project", TargetID: "7"},
	} {
		if _, err := service.Upsert(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	page, err := service.ListPage(context.Background(), RelationFilter{
		NodeTypes: []string{"okr_kr", "okr_point"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items = %#v, want two OKR-touching edges", page.Items)
	}
}
