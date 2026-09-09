package main

import (
	"errors"
	"strconv"
	"testing"

	"jarvis/internal/domain"
	"jarvis/internal/worldprogress"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateProjectWorldProgressSubject(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Project{}); err != nil {
		t.Fatal(err)
	}
	active := domain.Project{Name: "进行中的项目", Role: "owner", Status: "active", Priority: 1}
	archived := domain.Project{Name: "已归档项目", Role: "owner", Status: "archived", Priority: 1}
	if err := db.Create(&active).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&archived).Error; err != nil {
		t.Fatal(err)
	}

	if err := validateProjectWorldProgressSubject(t.Context(), db, " "+formatUint(active.ID)+" "); err != nil {
		t.Fatalf("active project rejected: %v", err)
	}
	for _, subjectID := range []string{"", "not-a-number", "0", formatUint(archived.ID), "999999"} {
		if err := validateProjectWorldProgressSubject(t.Context(), db, subjectID); !errors.Is(err, worldprogress.ErrNotFound) {
			t.Fatalf("subject_id=%q error=%v, want not found", subjectID, err)
		}
	}
}

func formatUint(value uint64) string {
	return strconv.FormatUint(value, 10)
}
