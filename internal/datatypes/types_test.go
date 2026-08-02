package datatypes

import (
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fixture struct {
	ID      uint64 `gorm:"primaryKey"`
	Payload JSON
	Day     Date
}

func TestSQLiteRoundTrip(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&fixture{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	wantDay := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	want := fixture{Payload: JSON(`{"answer":42}`), Day: Date(wantDay)}
	if err := db.Create(&want).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	var got fixture
	if err := db.First(&got, want.ID).Error; err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Payload.String() != want.Payload.String() {
		t.Fatalf("Payload = %s, want %s", got.Payload, want.Payload)
	}
	if !time.Time(got.Day).Equal(wantDay) {
		t.Fatalf("Day = %s, want %s", time.Time(got.Day), wantDay)
	}
}

func TestJSONRejectsInvalidValue(t *testing.T) {
	if _, err := JSON(`not-json`).Value(); err == nil {
		t.Fatal("invalid JSON Value() error = nil")
	}
}
