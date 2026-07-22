package progress

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestBackfillTaskSnapshotsValidatesInputs(t *testing.T) {
	t.Parallel()
	if _, err := BackfillTaskSnapshots(context.Background(), nil, time.Now()); err == nil {
		t.Fatal("nil db error = nil")
	}
	if _, err := BackfillTaskSnapshots(context.Background(), &gorm.DB{}, time.Time{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero cutover error = %v, want ErrInvalidInput", err)
	}
}
