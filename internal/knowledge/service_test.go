package knowledge

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
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

func TestPrepareCreateNormalizesPeriodToUTC(t *testing.T) {
	t.Parallel()
	shanghai := time.FixedZone("CST", 8*3600)
	from := time.Date(2026, 3, 1, 9, 0, 0, 0, shanghai)
	until := time.Date(2026, 7, 15, 18, 30, 0, 0, shanghai)
	prepared, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityPerson, ID: 7},
		EntityB:     EntityRef{Type: EntityProject, ID: 8},
		Description: "张三曾负责这个项目，7 月中交接给李四。",
		ValidFrom:   &from,
		ValidUntil:  &until,
	})
	if err != nil {
		t.Fatalf("prepareCreate() error = %v", err)
	}
	if prepared.ValidFrom.Location() != time.UTC || prepared.ValidUntil.Location() != time.UTC {
		t.Fatalf("locations = %v / %v, want UTC", prepared.ValidFrom.Location(), prepared.ValidUntil.Location())
	}
	if !prepared.ValidFrom.Equal(from) || !prepared.ValidUntil.Equal(until) {
		t.Fatalf("instants shifted: %v / %v", prepared.ValidFrom, prepared.ValidUntil)
	}
}

func TestPrepareCreateAllowsOpenEndedPeriod(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	current, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityPerson, ID: 7},
		EntityB:     EntityRef{Type: EntityProject, ID: 8},
		Description: "张三从 3 月起负责这个项目。",
		ValidFrom:   &from,
	})
	if err != nil {
		t.Fatalf("prepareCreate() with open end error = %v", err)
	}
	if current.ValidUntil != nil {
		t.Fatalf("valid_until = %v, want nil for a still-current relationship", current.ValidUntil)
	}

	until := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	ended, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityPerson, ID: 7},
		EntityB:     EntityRef{Type: EntityProject, ID: 8},
		Description: "张三曾负责这个项目，起始时间不详。",
		ValidUntil:  &until,
	})
	if err != nil {
		t.Fatalf("prepareCreate() with unknown start error = %v", err)
	}
	if ended.ValidFrom != nil {
		t.Fatalf("valid_from = %v, want nil for an unknown start", ended.ValidFrom)
	}
}

func TestPrepareCreateRejectsInvertedPeriod(t *testing.T) {
	t.Parallel()
	from := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	until := from.Add(-time.Hour)
	_, err := prepareCreate(CreateInput{
		EntityA:     EntityRef{Type: EntityPerson, ID: 7},
		EntityB:     EntityRef{Type: EntityProject, ID: 8},
		Description: "结束早于开始，应当报错。",
		ValidFrom:   &from,
		ValidUntil:  &until,
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestSameInstantComparesAcrossZonesAndNil(t *testing.T) {
	t.Parallel()
	utc := time.Date(2026, 3, 1, 1, 0, 0, 0, time.UTC)
	shanghai := utc.In(time.FixedZone("CST", 8*3600))
	if !sameInstant(&utc, &shanghai) {
		t.Fatalf("same instant across zones reported as different")
	}
	if !sameInstant(nil, nil) {
		t.Fatalf("two cleared bounds reported as different")
	}
	if sameInstant(&utc, nil) || sameInstant(nil, &utc) {
		t.Fatalf("clearing a bound reported as unchanged")
	}
	other := utc.Add(time.Hour)
	if sameInstant(&utc, &other) {
		t.Fatalf("different instants reported as equal")
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

func TestServiceAcceptsKeyMatterEntity(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatalf("OpenSQLite() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}
	matter := domain.KeyMatter{Title: "法务口径", Status: "跟进中"}
	person := domain.Person{OpenID: "ou_key_matter", Name: "法务同学", Role: "key", PriorityWeight: 1, IsActive: true}
	if err := db.Create(&matter).Error; err != nil {
		t.Fatalf("create key matter: %v", err)
	}
	if err := db.Create(&person).Error; err != nil {
		t.Fatalf("create person: %v", err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	created, err := service.Create(t.Context(), CreateInput{
		EntityA:     EntityRef{Type: EntityKeyMatter, ID: matter.ID},
		EntityB:     EntityRef{Type: EntityPerson, ID: person.ID},
		Description: "法务同学负责给出最终口径。",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	labels := map[EntityType]string{created.EntityA.Type: created.EntityA.Label, created.EntityB.Type: created.EntityB.Label}
	if labels[EntityKeyMatter] != matter.Title {
		t.Fatalf("key matter label = %q, want %q", labels[EntityKeyMatter], matter.Title)
	}
}
