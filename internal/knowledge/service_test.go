package knowledge

import (
	"errors"
	"testing"
)

func TestPrepareCreateCanonicalizesPair(t *testing.T) {
	t.Parallel()
	prepared, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityProject, ID: 8},
		EntityB:     EntityRef{Type: EntityPerson, ID: 7},
		Description: "  张三负责这个项目。  ",
	})
	if err != nil {
		t.Fatalf("prepareCreate() error = %v", err)
	}
	if prepared.EntityA.Type != EntityPerson || prepared.EntityA.ID != 7 || prepared.EntityB.Type != EntityProject || prepared.EntityB.ID != 8 {
		t.Fatalf("canonical pair = %#v / %#v", prepared.EntityA, prepared.EntityB)
	}
	if prepared.Description != "张三负责这个项目。" {
		t.Fatalf("description = %q", prepared.Description)
	}
}

func TestPrepareCreateRejectsSelfRelation(t *testing.T) {
	t.Parallel()
	_, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityTask, ID: 1},
		EntityB:     EntityRef{Type: EntityTask, ID: 1},
		Description: "自己关联自己。",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestPrepareCreateRequiresDescription(t *testing.T) {
	t.Parallel()
	_, err := prepareCreate(CreateInput{
		EntityA: EntityRef{Type: EntityTask, ID: 1},
		EntityB: EntityRef{Type: EntityProject, ID: 2},
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestValidateFilterRequiresEntityPair(t *testing.T) {
	t.Parallel()
	entityType := EntityProject
	err := validateFilter(FactFilter{EntityType: &entityType, Page: 1, PageSize: 20})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}
