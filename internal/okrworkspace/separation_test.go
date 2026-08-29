package okrworkspace

import (
	"testing"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openWorkspaceTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestCoreAndWeeklyWritesHaveSeparateOwnership(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-1", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-1", ObjectiveID: objective.ID, Title: "旧标题", OwnerName: "旧负责人", Priority: "p1"}
	metric := domain.KRMetric{ID: "metric-1", KRID: kr.ID, Text: "旧指标", Light: domain.LightGreen}
	point := domain.KRPoint{ID: "point-1", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "稳定拆解"}
	current := domain.KRProgress{ID: "progress-current", PointID: point.ID, Week: "2026-W35", Status: domain.StatusInProgress, Text: "旧本周进展"}
	history := domain.KRProgress{ID: "progress-history", PointID: point.ID, Week: "2026-W34", Status: domain.StatusDone, Text: "历史进展"}
	for _, value := range []any{&objective, &kr, &metric, &point, &current, &history} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, week := range []string{"2026-W34", "2026-W35"} {
		if err := db.Create(&domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: week, OpenedBy: "test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	coreBoard, err := service.CoreBoard(t.Context(), "2026-Q3")
	if err != nil {
		t.Fatal(err)
	}
	if len(coreBoard.Objectives[0].KRs[0].Points[0].Entries) != 0 || len(coreBoard.Objectives[0].KRs[0].Points[0].PreviousEntries) != 0 {
		t.Fatalf("core board leaked weekly progress: %+v", coreBoard)
	}

	core, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0, Title: "新 OKR 标题", Priority: "p0", MetricNote: "季度口径",
		Owners:  []OwnerView{{OpenID: "ou_a", Name: "甲"}, {OpenID: "ou_b", Name: "乙"}},
		Metrics: []MetricView{{ID: metric.ID, Text: "新核心指标", Light: domain.LightYellow}},
		Points:  []PointView{{ID: point.ID, Kind: point.Kind, Title: point.Title}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if core.Version != 1 || len(core.Owners) != 2 {
		t.Fatalf("core result = %+v", core)
	}
	var progressAfterCore []domain.KRProgress
	if err := db.Order("week").Find(&progressAfterCore).Error; err != nil {
		t.Fatal(err)
	}
	if len(progressAfterCore) != 2 || progressAfterCore[0].Text != "历史进展" || progressAfterCore[1].Text != "旧本周进展" {
		t.Fatalf("core write changed weekly progress: %+v", progressAfterCore)
	}

	weekly, err := service.ReplaceWeeklyProgress(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 1, Week: "2026-W35", Title: "不应覆盖", OwnerName: "不应覆盖", Priority: "p2",
		Metrics: []MetricView{{ID: metric.ID, Text: "不应覆盖", Light: domain.LightRed}},
		Points:  []PointView{{ID: point.ID, Kind: point.Kind, Title: "不应覆盖", Entries: []ProgressView{{ID: current.ID, Status: domain.StatusDone, Text: "新本周进展"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if weekly.Title != "新 OKR 标题" || weekly.OwnerName != "甲、乙" || weekly.Metrics[0].Text != "新核心指标" || weekly.Points[0].Title != "稳定拆解" {
		t.Fatalf("weekly write changed OKR definition: %+v", weekly)
	}
	var persistedHistory domain.KRProgress
	if err := db.First(&persistedHistory, "id = ?", history.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persistedHistory.Text != "历史进展" || weekly.Points[0].Entries[0].Text != "新本周进展" {
		t.Fatalf("weekly/history result = %+v / %+v", weekly.Points[0].Entries, persistedHistory)
	}

	preview, err := service.ReminderPreview(t.Context(), "2026-Q3", "2026-W35")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Summary.OwnerCount != 2 || len(preview.Recipients) != 2 {
		t.Fatalf("multi-owner reminder preview = %+v", preview)
	}
	if err := service.DeleteKR(t.Context(), kr.ID, DeleteKRInput{ExpectedVersion: weekly.Version}); err != nil {
		t.Fatal(err)
	}
	var preservedProgress int64
	if err := db.Model(&domain.KRProgress{}).Where("point_id = ?", point.ID).Count(&preservedProgress).Error; err != nil {
		t.Fatal(err)
	}
	if preservedProgress != 2 {
		t.Fatalf("core delete removed weekly history: count=%d", preservedProgress)
	}
}

func TestWeeklyReportWeekLifecycleDoesNotRequireProgress(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-week", Title: "增长", Quarter: "2026-Q3"}
	if err := db.Create(&objective).Error; err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	opened, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", OpenedBy: "ou_owner"})
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Created || opened.Week.Week != "2026-W36" || opened.Week.OpenedBy != "ou_owner" {
		t.Fatalf("opened week = %+v", opened)
	}
	again, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", OpenedBy: "ou_other"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Week.OpenedBy != "ou_owner" {
		t.Fatalf("idempotent open = %+v", again)
	}
	scope, err := service.LatestWeeklyScope(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if scope.Quarter != objective.Quarter || scope.Week != "2026-W36" {
		t.Fatalf("scope = %+v", scope)
	}
	board, err := service.Board(t.Context(), objective.Quarter, "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	if len(board.AvailableWeeks) != 1 || board.AvailableWeeks[0] != "2026-W36" || len(board.Objectives) != 1 {
		t.Fatalf("board = %+v", board)
	}
	var progressCount int64
	if err := db.Model(&domain.KRProgress{}).Count(&progressCount).Error; err != nil {
		t.Fatal(err)
	}
	if progressCount != 0 {
		t.Fatalf("opening week created %d progress rows", progressCount)
	}
}

func TestCoreWorkspaceStartsWithoutWeeklyReportSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(t.Context(), CreateObjectiveInput{Quarter: "2026-Q3", Title: "建立通用 OKR 能力"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateKR(t.Context(), objective.ID, CreateKRInput{Title: "核心模块不依赖周报", Priority: "p0", CreatedBy: "ou_owner"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Title != "核心模块不依赖周报" || created.Version != 0 {
		t.Fatalf("created KR = %+v", created)
	}
	scope, err := service.LatestCoreScope(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if scope.Quarter != "2026-Q3" {
		t.Fatalf("core scope = %+v", scope)
	}
	board, err := service.CoreBoard(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if board.Quarter != "2026-Q3" || board.Week != "" || len(board.AvailableWeeks) != 0 || len(board.Objectives) != 1 || len(board.Objectives[0].KRs) != 1 {
		t.Fatalf("core board = %+v", board)
	}
	updated, err := service.ReplaceKRCore(t.Context(), created.ID, ReplaceKRInput{
		ExpectedVersion: 0, Title: created.Title, Priority: "p0",
		Metrics: []MetricView{{ID: "metric-1", Text: "核心指标", Light: domain.LightGreen}},
		Points:  []PointView{{ID: "point-1", Kind: domain.PointKindStrategy, Title: "关键路径"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err = service.ReplaceKRCore(t.Context(), created.ID, ReplaceKRInput{ExpectedVersion: updated.Version, Title: created.Title, Priority: "p0"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteKR(t.Context(), created.ID, DeleteKRInput{ExpectedVersion: updated.Version}); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateCorePreservesLegacyOwnerRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := []string{
		`CREATE TABLE okr_workspace_kr_owner (kr_id text, person_id integer, open_id text NOT NULL DEFAULT '', name text NOT NULL, PRIMARY KEY (kr_id, person_id))`,
		`CREATE INDEX idx_okr_workspace_kr_owner_person_id ON okr_workspace_kr_owner(person_id)`,
		`INSERT INTO okr_workspace_kr_owner (kr_id, person_id, open_id, name) VALUES ('kr-1', 11, 'ou_a', '甲'), ('kr-1', 12, 'ou_b', '乙')`,
	}
	for _, statement := range legacy {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	var owners []domain.KROwner
	if err := db.Order("person_id").Find(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 || owners[0].Name != "甲" || owners[1].Name != "乙" {
		t.Fatalf("legacy owners were not preserved: %+v", owners)
	}
	if !db.Migrator().HasColumn(&domain.KROwner{}, "owner_key") || !db.Migrator().HasColumn(&domain.KROwner{}, "sort_order") {
		t.Fatal("owner compatibility columns were not added")
	}
}
