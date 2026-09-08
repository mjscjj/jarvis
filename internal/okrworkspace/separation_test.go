package okrworkspace

import (
	"errors"
	"reflect"
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

func TestCoreSubjectExistsSupportsAllOKRLevels(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-subject", Title: "目标", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-subject", ObjectiveID: objective.ID, Title: "关键结果"}
	point := domain.KRPoint{ID: "point-subject", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "子 KR"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct{ subjectType, subjectID string }{
		{"okr_objective", objective.ID}, {"okr_kr", kr.ID}, {"okr_point", point.ID},
	} {
		exists, err := service.CoreSubjectExists(t.Context(), testCase.subjectType, testCase.subjectID)
		if err != nil || !exists {
			t.Fatalf("CoreSubjectExists(%s, %s) = %t, %v", testCase.subjectType, testCase.subjectID, exists, err)
		}
	}
	exists, err := service.CoreSubjectExists(t.Context(), "okr_kr", "missing")
	if err != nil || exists {
		t.Fatalf("CoreSubjectExists(missing) = %t, %v", exists, err)
	}
	if _, err := service.CoreSubjectExists(t.Context(), "project", "1"); err == nil {
		t.Fatal("CoreSubjectExists(project) error=nil, want unsupported type")
	}
}

func TestCoreSubjectExistsRejectsPlanDraftSubjects(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-plan-subject", PlanID: "plan-1", Title: "草稿目标", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-plan-subject", ObjectiveID: objective.ID, Title: "草稿 KR"}
	point := domain.KRPoint{ID: "point-plan-subject", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "草稿子 KR"}
	for _, value := range []any{&objective, &kr, &point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct{ subjectType, subjectID string }{
		{"okr_objective", objective.ID}, {"okr_kr", kr.ID}, {"okr_point", point.ID},
	} {
		exists, err := service.CoreSubjectExists(t.Context(), testCase.subjectType, testCase.subjectID)
		if err != nil || exists {
			t.Fatalf("CoreSubjectExists(%s, %s) = %t, %v; want false", testCase.subjectType, testCase.subjectID, exists, err)
		}
	}
}

// Both quarter boards read the same objectives and KRs; only the Biz one may
// carry labels and legacy Meego links. They share one loader, so this pins the
// projection difference rather than each board's own row assembly.
func TestCoreAndBizBoardsShareDefinitionsAndDifferOnlyByBizFields(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-board", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-board", ObjectiveID: objective.ID, Title: "一级 KR"}
	point := domain.KRPoint{ID: "point-board", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "策略要点", MeegoWorkItemID: "wi-7", MeegoURL: "https://meego.example/wi-7"}
	for _, value := range []any{
		&objective, &kr, &point,
		&domain.KROwner{KRID: kr.ID, OpenID: "ou_owner", Name: "负责人"},
		&domain.KRTag{KRID: kr.ID, Type: domain.TagTypeBusinessCategory, Value: "公会业务"},
		&domain.PointTag{PointID: point.ID, Type: "custom", Value: "双周报"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	core, err := service.CoreBoard(t.Context(), "2026-Q3")
	if err != nil {
		t.Fatal(err)
	}
	biz, err := service.BizCoreBoard(t.Context(), "2026-Q3")
	if err != nil {
		t.Fatal(err)
	}
	coreKR := core.Objectives[0].KRs[0]
	bizKR := biz.Objectives[0].KRs[0]
	if coreKR.ID != bizKR.ID || coreKR.Title != bizKR.Title || !reflect.DeepEqual(coreKR.Owners, bizKR.Owners) {
		t.Fatalf("boards disagree on the shared definition: core=%+v biz=%+v", coreKR, bizKR)
	}
	if len(coreKR.Tags) != 0 || len(coreKR.Points[0].Tags) != 0 || coreKR.Points[0].MeegoWorkItemID != "" || coreKR.Points[0].MeegoURL != "" {
		t.Fatalf("generic core board leaked Biz fields: %+v", coreKR)
	}
	if len(bizKR.Tags) != 1 || len(bizKR.Points[0].Tags) != 1 || bizKR.Points[0].MeegoWorkItemID != "wi-7" {
		t.Fatalf("Biz core board lost Biz fields: %+v", bizKR)
	}
}

func TestCoreAndWeeklyWritesHaveSeparateOwnership(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-1", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-1", ObjectiveID: objective.ID, Title: "旧标题"}
	metric := domain.KRMetric{ID: "metric-1", KRID: kr.ID, Text: "旧指标", Light: domain.LightGreen}
	point := domain.KRPoint{ID: "point-1", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "稳定拆解"}
	current := domain.KRProgress{ID: "progress-current", PointID: point.ID, Week: "2026-W35", Status: domain.StatusInProgress, Text: "旧本周进展"}
	history := domain.KRProgress{ID: "progress-history", PointID: point.ID, Week: "2026-W34", Status: domain.StatusDone, Text: "历史进展"}
	businessTag := domain.KRTag{KRID: kr.ID, Type: domain.TagTypeBusinessCategory, Value: "公会业务"}
	priorityTag := domain.KRTag{KRID: kr.ID, Type: domain.TagTypePriority, Value: "p1"}
	for _, value := range []any{&objective, &kr, &metric, &point, &current, &history, &businessTag, &priorityTag} {
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
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0, Title: kr.Title,
		Points: []PointView{{ID: point.ID, Kind: point.Kind, Title: point.Title, Tags: []TagView{}, Entries: []ProgressView{{ID: "forbidden", Status: domain.StatusDone, Text: "不应进入核心写接口"}}}},
		Tags:   []TagView{{Type: domain.TagTypeBusinessCategory, Value: "公会业务"}, {Type: domain.TagTypePriority, Value: "p1"}},
	}); err == nil {
		t.Fatal("ReplaceKRCore() accepted weekly progress")
	}
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0, Title: kr.Title,
		Points: []PointView{{ID: point.ID, Kind: point.Kind, Title: point.Title}},
		Tags:   []TagView{{Type: domain.TagTypeBusinessCategory, Value: "公会业务"}, {Type: domain.TagTypePriority, Value: "p1"}},
	}); err == nil {
		t.Fatal("ReplaceKRCore() accepted a point without explicit tags")
	}

	core, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0, Title: "新 OKR 标题", MetricNote: "季度口径",
		Owners:  []OwnerView{{OpenID: "ou_a", Name: "甲"}, {OpenID: "ou_b", Name: "乙"}},
		Metrics: []MetricView{{ID: metric.ID, Text: "新核心指标", Light: domain.LightYellow}},
		Points:  []PointView{{ID: point.ID, Kind: point.Kind, Title: point.Title, Tags: []TagView{{Type: "management_focus", Value: "策略要点"}}}},
		Tags:    []TagView{{Type: domain.TagTypeBusinessCategory, Value: "公会业务"}, {Type: domain.TagTypePriority, Value: "p0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if core.Version != 1 || len(core.Owners) != 2 {
		t.Fatalf("core result = %+v", core)
	}
	if len(core.Tags) != 2 || core.Tags[0].Type != domain.TagTypeBusinessCategory || core.Tags[1].Value != "p0" {
		t.Fatalf("structural tags = %+v", core.Tags)
	}
	if !reflect.DeepEqual(core.Points[0].Tags, []TagView{{Type: "management_focus", Value: "策略要点"}}) {
		t.Fatalf("point tags = %+v", core.Points[0].Tags)
	}
	var progressAfterCore []domain.KRProgress
	if err := db.Order("week").Find(&progressAfterCore).Error; err != nil {
		t.Fatal(err)
	}
	if len(progressAfterCore) != 2 || progressAfterCore[0].Text != "历史进展" || progressAfterCore[1].Text != "旧本周进展" {
		t.Fatalf("core write changed weekly progress: %+v", progressAfterCore)
	}

	weekly, err := service.UpdateProgressEntry(t.Context(), current.ID, ProgressEntryInput{
		ExpectedVersion: 0, Week: "2026-W35", Status: domain.StatusDone, Text: "新本周进展", Source: "manual", UpdatedBy: "ou_editor",
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
	// Deleting a KR takes its weekly progress with it. Every week renders from
	// the same point rows, so leaving the progress behind would only strand it.
	if err := service.DeleteKR(t.Context(), kr.ID, DeleteKRInput{ExpectedVersion: core.Version}); err != nil {
		t.Fatal(err)
	}
	var remainingProgress, remainingPoints, remainingKR int64
	if err := db.Model(&domain.KRProgress{}).Where("point_id = ?", point.ID).Count(&remainingProgress).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KRPoint{}).Where("kr_id = ?", kr.ID).Count(&remainingPoints).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KR{}).Where("id = ?", kr.ID).Count(&remainingKR).Error; err != nil {
		t.Fatal(err)
	}
	if remainingProgress != 0 || remainingPoints != 0 || remainingKR != 0 {
		t.Fatalf("core delete left rows behind: progress=%d points=%d kr=%d", remainingProgress, remainingPoints, remainingKR)
	}
}

func TestWeeklyCoreDataIsIsolatedByWeek(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-weekly-core", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-weekly-core", ObjectiveID: objective.ID, Title: "提升转化", MetricNote: "季度模板"}
	metric := domain.KRMetric{ID: "metric-weekly-core", KRID: kr.ID, Text: "季度累计 100", Light: domain.LightGreen}
	for _, value := range []any{&objective, &kr, &metric} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, week := range []string{"2026-W35", "2026-W36"} {
		if err := db.Create(&domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: week, OpenedBy: "test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	w35, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		Week: "2026-W35", MetricNote: "W35 数据", UpdatedBy: "ou_editor",
		Metrics: []MetricView{{ID: metric.ID, Text: "本周累计 120", Light: domain.LightYellow}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if w35.MetricNote != "W35 数据" || w35.Metrics[0].Text != "本周累计 120" || w35.Metrics[0].Light != domain.LightYellow {
		t.Fatalf("W35 weekly core = %+v", w35)
	}

	w36Board, err := service.Board(t.Context(), objective.Quarter, "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	w36 := w36Board.Objectives[0].KRs[0]
	if w36.MetricNote != "季度模板" || w36.Metrics[0].Text != "季度累计 100" || w36.WeeklyCoreVersion != 0 {
		t.Fatalf("W36 inherited another week's edit: %+v", w36)
	}

	coreBoard, err := service.CoreBoard(t.Context(), objective.Quarter)
	if err != nil {
		t.Fatal(err)
	}
	core := coreBoard.Objectives[0].KRs[0]
	if core.MetricNote != "季度模板" || core.Metrics[0].Text != "季度累计 100" {
		t.Fatalf("weekly write changed OKR core: %+v", core)
	}

	updated, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		ExpectedVersion: w35.WeeklyCoreVersion, Week: "2026-W35", MetricNote: "W35 复盘", UpdatedBy: "ou_editor",
		Metrics: []MetricView{{ID: metric.ID, Text: "本周累计 125", Light: domain.LightRed}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.WeeklyCoreVersion != 1 || updated.MetricNote != "W35 复盘" || updated.Metrics[0].Text != "本周累计 125" {
		t.Fatalf("updated W35 weekly core = %+v", updated)
	}
	if _, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		ExpectedVersion: 0, Week: "2026-W35", MetricNote: "stale",
		Metrics: []MetricView{{ID: metric.ID, Text: "stale", Light: domain.LightGreen}},
	}); err != ErrConflict {
		t.Fatalf("stale weekly core write error = %v, want conflict", err)
	}
}

func TestWeeklyCoreCanSeedFirstMetricWithoutChangingDefinitionOrOtherWeeks(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-weekly-empty-metric", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-weekly-empty-metric", ObjectiveID: objective.ID, Title: "提升转化"}
	for _, value := range []any{&objective, &kr} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, week := range []string{"2026-W35", "2026-W36"} {
		if err := db.Create(&domain.WeeklyReportWeek{Quarter: objective.Quarter, Week: week, OpenedBy: "test"}).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	w35, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		Week: "2026-W35", UpdatedBy: "ou_editor",
		Metrics: []MetricView{{ID: "weekly-metric-1", Light: domain.LightYellow, Images: []domain.ImageRef{{ID: "img-1", Name: "核心数据.png", URL: "/okr-assets/img-1.png"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(w35.Metrics) != 1 || w35.Metrics[0].ID != "weekly-metric-1" || len(w35.Metrics[0].Images) != 1 || w35.Metrics[0].Images[0].ID != "img-1" {
		t.Fatalf("W35 weekly metric = %+v", w35.Metrics)
	}

	w36Board, err := service.Board(t.Context(), objective.Quarter, "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	if got := w36Board.Objectives[0].KRs[0].Metrics; len(got) != 0 {
		t.Fatalf("W36 inherited W35-only metric: %+v", got)
	}
	coreBoard, err := service.CoreBoard(t.Context(), objective.Quarter)
	if err != nil {
		t.Fatal(err)
	}
	if got := coreBoard.Objectives[0].KRs[0].Metrics; len(got) != 0 {
		t.Fatalf("weekly metric changed OKR definition: %+v", got)
	}

	if _, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		Week: "2026-W36", UpdatedBy: "ou_editor",
		Metrics: []MetricView{
			{ID: "weekly-metric-2", Text: "first", Light: domain.LightGreen},
			{ID: "weekly-metric-3", Text: "second", Light: domain.LightGreen},
		},
	}); err == nil {
		t.Fatal("seeding more than one weekly metric should fail")
	}
	if _, err := service.ReplaceWeeklyKRCore(t.Context(), kr.ID, WeeklyKRCoreInput{
		Week: "2026-W36", UpdatedBy: "ou_editor",
		Metrics: []MetricView{{ID: "weekly-metric-empty", Light: domain.LightGreen}},
	}); err == nil {
		t.Fatal("empty weekly metric should fail")
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

	opened, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateClassic, OpenedBy: "ou_owner"})
	if err != nil {
		t.Fatal(err)
	}
	if !opened.Created || opened.Week.Week != "2026-W36" || opened.Week.TemplateKey != domain.WeekTemplateClassic || opened.Week.OpenedBy != "ou_owner" {
		t.Fatalf("opened week = %+v", opened)
	}
	again, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateClassic, OpenedBy: "ou_other"})
	if err != nil {
		t.Fatal(err)
	}
	if again.Created || again.Week.OpenedBy != "ou_owner" {
		t.Fatalf("idempotent open = %+v", again)
	}
	if _, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: objective.Quarter, Week: "2026-W36", TemplateKey: domain.WeekTemplateOKRPreview, OpenedBy: "ou_other"}); !errors.Is(err, ErrWeekTemplateConflict) {
		t.Fatalf("template-changing open error = %v, want ErrWeekTemplateConflict", err)
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
	if board.TemplateKey != domain.WeekTemplateClassic || len(board.AvailableWeeks) != 1 || board.AvailableWeeks[0] != "2026-W36" || len(board.Objectives) != 1 {
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

func TestDeleteWeekRemovesOnlySelectedWeeklyReportScope(t *testing.T) {
	db := openWorkspaceTestDB(t)
	q2Objective := domain.Objective{ID: "o-delete-q2", Title: "测试季度", Quarter: "2026-Q2"}
	q3Objective := domain.Objective{ID: "o-delete-q3", Title: "正式季度", Quarter: "2026-Q3"}
	q2KR := domain.KR{ID: "kr-delete-q2", ObjectiveID: q2Objective.ID, Title: "测试 KR"}
	q3KR := domain.KR{ID: "kr-delete-q3", ObjectiveID: q3Objective.ID, Title: "正式 KR"}
	q2Point := domain.KRPoint{ID: "point-delete-q2", KRID: q2KR.ID, Kind: domain.PointKindStrategy, Title: "测试拆解"}
	q3Point := domain.KRPoint{ID: "point-delete-q3", KRID: q3KR.ID, Kind: domain.PointKindProduct, Title: "正式拆解"}
	for _, value := range []any{&q2Objective, &q3Objective, &q2KR, &q3KR, &q2Point, &q3Point} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	for _, value := range []any{
		&domain.WeeklyReportWeek{Quarter: "2026-Q2", Week: "2026-W14", OpenedBy: "test"},
		&domain.WeeklyReportWeek{Quarter: "2026-Q2", Week: "2026-W15", OpenedBy: "test"},
		&domain.WeeklyReportWeek{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "owner"},
		&domain.WeeklyKRCore{KRID: q2KR.ID, Week: "2026-W14", MetricNote: "保留"},
		&domain.WeeklyKRCore{KRID: q2KR.ID, Week: "2026-W15", MetricNote: "删除"},
		&domain.KRProgress{ID: "progress-q2-w14", PointID: q2Point.ID, Week: "2026-W14", Status: domain.StatusDone, Text: "保留"},
		&domain.KRProgress{ID: "progress-q2-w15", PointID: q2Point.ID, Week: "2026-W15", Status: domain.StatusInProgress, Text: "删除"},
		&domain.KRProgress{ID: "progress-q3-w35", PointID: q3Point.ID, Week: "2026-W35", Status: domain.StatusInProgress, Text: "保留正式数据"},
		&domain.PageComment{ID: "comment-q2-w14", Quarter: "2026-Q2", Week: "2026-W14", Content: "保留"},
		&domain.PageComment{ID: "comment-q2-w15", Quarter: "2026-Q2", Week: "2026-W15", Content: "删除"},
		&domain.PageComment{ID: "comment-q3-w35", Quarter: "2026-Q3", Week: "2026-W35", Content: "保留正式评论"},
		&domain.MeegoSyncSnapshot{PointID: q2Point.ID, Week: "2026-W15"},
		&domain.MeegoSyncSnapshot{PointID: q3Point.ID, Week: "2026-W35"},
		&domain.ReminderBatch{ID: "batch-q2-w14", Quarter: "2026-Q2", Week: "2026-W14"},
		&domain.ReminderBatch{ID: "batch-q2-w15", Quarter: "2026-Q2", Week: "2026-W15"},
		&domain.ReminderBatch{ID: "batch-q3-w35", Quarter: "2026-Q3", Week: "2026-W35"},
	} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := service.DeleteWeek(t.Context(), "2026-Q2", "2026-W15")
	if err != nil {
		t.Fatal(err)
	}
	if deleted.NextWeek != "2026-W14" {
		t.Fatalf("next week = %q, want 2026-W14", deleted.NextWeek)
	}
	if deleted.Deleted.WeeklyCores != 1 || deleted.Deleted.Progress != 1 || deleted.Deleted.Comments != 1 || deleted.Deleted.MeegoSnapshots != 1 || deleted.Deleted.ReminderBatches != 1 {
		t.Fatalf("deleted counts = %+v", deleted.Deleted)
	}

	assertCount := func(model any, query string, args []any, want int64) {
		t.Helper()
		var got int64
		if err := db.Model(model).Where(query, args...).Count(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("%T count for %q = %d, want %d", model, query, got, want)
		}
	}
	assertCount(&domain.WeeklyReportWeek{}, "quarter = ? AND week = ?", []any{"2026-Q2", "2026-W15"}, 0)
	assertCount(&domain.WeeklyKRCore{}, "kr_id = ? AND week = ?", []any{q2KR.ID, "2026-W15"}, 0)
	assertCount(&domain.KRProgress{}, "point_id = ? AND week = ?", []any{q2Point.ID, "2026-W15"}, 0)
	assertCount(&domain.PageComment{}, "quarter = ? AND week = ?", []any{"2026-Q2", "2026-W15"}, 0)
	assertCount(&domain.MeegoSyncSnapshot{}, "point_id = ? AND week = ?", []any{q2Point.ID, "2026-W15"}, 0)
	assertCount(&domain.ReminderBatch{}, "quarter = ? AND week = ?", []any{"2026-Q2", "2026-W15"}, 0)

	assertCount(&domain.WeeklyReportWeek{}, "quarter = ? AND week = ?", []any{"2026-Q2", "2026-W14"}, 1)
	assertCount(&domain.KRProgress{}, "point_id = ? AND week = ?", []any{q2Point.ID, "2026-W14"}, 1)
	assertCount(&domain.WeeklyReportWeek{}, "quarter = ? AND week = ?", []any{"2026-Q3", "2026-W35"}, 1)
	assertCount(&domain.KRProgress{}, "point_id = ? AND week = ?", []any{q3Point.ID, "2026-W35"}, 1)
	assertCount(&domain.PageComment{}, "quarter = ? AND week = ?", []any{"2026-Q3", "2026-W35"}, 1)
	assertCount(&domain.Objective{}, "id IN ?", []any{[]string{q2Objective.ID, q3Objective.ID}}, 2)
	assertCount(&domain.KR{}, "id IN ?", []any{[]string{q2KR.ID, q3KR.ID}}, 2)
	assertCount(&domain.KRPoint{}, "id IN ?", []any{[]string{q2Point.ID, q3Point.ID}}, 2)

	if _, err := service.DeleteWeek(t.Context(), "2026-Q2", "2026-W15"); err != ErrWeekNotFound {
		t.Fatalf("delete missing week error = %v, want ErrWeekNotFound", err)
	}
}

func TestBoardsSwitchQuarterWithoutMixingWeeklyScopes(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objectives := []domain.Objective{
		{ID: "o-q2", Title: "测试季度", Quarter: "2026-Q2"},
		{ID: "o-q3", Title: "正式季度", Quarter: "2026-Q3"},
	}
	for index := range objectives {
		if err := db.Create(&objectives[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	weeks := []domain.WeeklyReportWeek{
		{Quarter: "2026-Q2", Week: "2026-W14", OpenedBy: "test"},
		{Quarter: "2026-Q2", Week: "2026-W15", OpenedBy: "test"},
		{Quarter: "2026-Q3", Week: "2026-W35", OpenedBy: "test"},
	}
	for index := range weeks {
		if err := db.Create(&weeks[index]).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}

	core, err := service.CoreBoard(t.Context(), "2026-Q2")
	if err != nil {
		t.Fatal(err)
	}
	if core.Quarter != "2026-Q2" || len(core.Objectives) != 1 || core.Objectives[0].ID != "o-q2" {
		t.Fatalf("Q2 core board = %+v", core)
	}
	if len(core.AvailableQuarters) != 2 || core.AvailableQuarters[0] != "2026-Q3" || core.AvailableQuarters[1] != "2026-Q2" {
		t.Fatalf("available quarters = %+v", core.AvailableQuarters)
	}

	weekly, err := service.Board(t.Context(), "2026-Q2", "")
	if err != nil {
		t.Fatal(err)
	}
	if weekly.Quarter != "2026-Q2" || weekly.Week != "2026-W15" || len(weekly.Objectives) != 1 || weekly.Objectives[0].ID != "o-q2" {
		t.Fatalf("Q2 weekly board mixed another quarter: %+v", weekly)
	}
	if len(weekly.AvailableWeeks) != 2 || weekly.AvailableWeeks[0] != "2026-W15" || weekly.AvailableWeeks[1] != "2026-W14" {
		t.Fatalf("Q2 available weeks = %+v", weekly.AvailableWeeks)
	}

	latest, err := service.CoreBoard(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	if latest.Quarter != "2026-Q3" || len(latest.Objectives) != 1 || latest.Objectives[0].ID != "o-q3" {
		t.Fatalf("latest core board = %+v", latest)
	}
}

func TestMigrateCoreBackfillsHistoricalProgressScopes(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-history", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-history", ObjectiveID: objective.ID, Title: "历史 KR"}
	point := domain.KRPoint{ID: "point-history", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "历史拆解"}
	progress := domain.KRProgress{ID: "progress-history", PointID: point.ID, Week: "2026-W34", Status: domain.StatusDone, Text: "历史进展"}
	for _, value := range []any{&objective, &kr, &point, &progress} {
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	var week domain.WeeklyReportWeek
	if err := db.First(&week, "quarter = ? AND week = ?", objective.Quarter, progress.Week).Error; err != nil {
		t.Fatal(err)
	}
	if week.OpenedBy != "migration" || week.OpenedAt.IsZero() {
		t.Fatalf("backfilled week = %+v", week)
	}

	if err := MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := db.Model(&domain.WeeklyReportWeek{}).Where("quarter = ? AND week = ?", objective.Quarter, progress.Week).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("backfill was not idempotent: count=%d", count)
	}
}

func TestMigrateBizOKRRejectsLegacyPlanContentSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE okr_workspace_plan (
		id text PRIMARY KEY,
		quarter text NOT NULL,
		title text NOT NULL,
		content JSON NOT NULL
	)`).Error; err != nil {
		t.Fatal(err)
	}

	err = MigrateBizOKR(db)
	if err == nil || err.Error() != "migrate Biz OKR module: legacy okr_workspace_plan.content column is unsupported" {
		t.Fatalf("MigrateBizOKR error = %v", err)
	}
}

func TestCoreWorkspaceSupportsFormalProgressWithoutBizSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := MigrateCore(db); err != nil {
		t.Fatal(err)
	}
	if db.Migrator().HasColumn(&domain.KR{}, "priority") {
		t.Fatal("fresh KR schema still has duplicate priority column")
	}
	for _, model := range []any{&domain.KRTag{}, &domain.PointTag{}, &domain.OKRPlan{}, &domain.WeeklyScore{}, &domain.PageComment{}, &domain.MeegoSyncSnapshot{}, &domain.ReminderBatch{}} {
		if db.Migrator().HasTable(model) {
			t.Fatalf("MigrateCore created Biz table for %T", model)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(t.Context(), CreateObjectiveInput{Quarter: "2026-Q3", Title: "建立通用 OKR 能力"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.CreateKR(t.Context(), objective.ID, CreateKRInput{
		Title: "核心模块不依赖周报", CreatedBy: "ou_owner",
	})
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
	decomposed, err := service.ReplaceGenericKRCore(t.Context(), created.ID, ReplaceGenericKRInput{
		ExpectedVersion: 0, Title: "更新后的通用 KR", MetricNote: "季度口径",
		Metrics: []MetricView{{ID: "metric-core-only", Text: "完成率 100%", Light: domain.LightGreen}},
		Points:  []GenericPointView{{ID: "point-core-only", Kind: domain.PointKindStrategy, Title: "完成通用拆解", Owners: []OwnerView{{OpenID: "ou_owner", Name: "负责人"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(decomposed.Metrics) != 1 || len(decomposed.Points) != 1 || decomposed.Points[0].Tags == nil {
		t.Fatalf("generic decomposition = %+v", decomposed)
	}
	if _, err := service.OpenWeek(t.Context(), OpenWeekInput{Quarter: "2026-Q3", Week: "2026-W36", TemplateKey: domain.WeekTemplateClassic}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateProgressEntry(t.Context(), "point-core-only", ProgressEntryInput{ID: "progress-core-only", Week: "2026-W36", Status: domain.StatusInProgress, Text: "通用正式进展"}); err != nil {
		t.Fatal(err)
	}
	progress, err := service.ProgressBoard(t.Context(), "2026-Q3", "2026-W36")
	if err != nil {
		t.Fatal(err)
	}
	if len(progress.Objectives[0].KRs[0].Points[0].Entries) != 1 || progress.Objectives[0].KRs[0].Points[0].Entries[0].Text != "通用正式进展" {
		t.Fatalf("generic progress board = %+v", progress)
	}
	if err := service.DeleteKR(t.Context(), created.ID, DeleteKRInput{ExpectedVersion: decomposed.Version}); err != nil {
		t.Fatal(err)
	}
}

func TestObjectiveCanBeRenamedAndOnlyDeletedWhenEmpty(t *testing.T) {
	db := openWorkspaceTestDB(t)
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	objective, err := service.CreateObjective(t.Context(), CreateObjectiveInput{Quarter: "2026-Q3", Title: "旧方向"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateObjective(t.Context(), objective.ID, UpdateObjectiveInput{Title: "新方向"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "新方向" {
		t.Fatalf("updated objective = %+v", updated)
	}
	kr, err := service.CreateKR(t.Context(), objective.ID, CreateKRInput{Title: "仍有关联 KR"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteObjective(t.Context(), objective.ID); err == nil {
		t.Fatal("DeleteObjective() succeeded while KRs still exist")
	}
	if err := service.DeleteKR(t.Context(), kr.ID, DeleteKRInput{ExpectedVersion: kr.Version}); err != nil {
		t.Fatal(err)
	}
	if err := service.DeleteObjective(t.Context(), objective.ID); err != nil {
		t.Fatal(err)
	}
}

func TestStructuralTagsRejectConflictingValues(t *testing.T) {
	for name, tags := range map[string][]TagView{
		"multiple businesses": {{Type: domain.TagTypeBusinessCategory, Value: "公会业务"}, {Type: domain.TagTypeBusinessCategory, Value: "运营效率"}},
		"multiple priorities": {{Type: domain.TagTypePriority, Value: "p0"}, {Type: domain.TagTypePriority, Value: "p1"}},
		"invalid priority":    {{Type: domain.TagTypePriority, Value: "focus"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateTags(tags); err == nil {
				t.Fatalf("validateTags(%+v) succeeded", tags)
			}
		})
	}
	if err := validateTags([]TagView{{Type: domain.TagTypeBusinessCategory, Value: "公会业务"}, {Type: domain.TagTypePriority, Value: "p0"}}); err != nil {
		t.Fatalf("valid structural tags: %v", err)
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

func TestMigrateCoreMovesLegacyOwnerProjectionToOwnerTable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	legacy := []string{
		`CREATE TABLE okr_workspace_kr (
			id text, objective_id text NOT NULL, title text NOT NULL,
			owner_open_id text NOT NULL DEFAULT "", owner_name text NOT NULL DEFAULT "",
			metric_note text NOT NULL DEFAULT "", sort_order integer NOT NULL DEFAULT 0,
			version integer NOT NULL DEFAULT 0, created_by text NOT NULL DEFAULT "", updated_by text NOT NULL DEFAULT "",
			created_at datetime NOT NULL, updated_at datetime NOT NULL, PRIMARY KEY (id)
		)`,
		`CREATE INDEX idx_okr_workspace_kr_objective_id ON okr_workspace_kr(objective_id)`,
		`CREATE INDEX idx_okr_workspace_kr_owner_open_id ON okr_workspace_kr(owner_open_id)`,
		`CREATE INDEX idx_okr_workspace_kr_owner_name ON okr_workspace_kr(owner_name)`,
		`INSERT INTO okr_workspace_kr (id, objective_id, title, owner_name, created_at, updated_at)
		 VALUES ('kr-legacy', 'o-1', '目标', '甲、乙', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
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
	if err := db.Where("kr_id = ?", "kr-legacy").Order("sort_order").Find(&owners).Error; err != nil {
		t.Fatal(err)
	}
	if len(owners) != 2 || owners[0].Name != "甲" || owners[1].Name != "乙" {
		t.Fatalf("migrated owners = %+v", owners)
	}
	if db.Migrator().HasColumn(&domain.KR{}, "owner_name") || db.Migrator().HasColumn(&domain.KR{}, "owner_open_id") {
		t.Fatal("legacy owner projection columns still exist")
	}
}

// Dropping a point from a KR takes its weekly rows with it. The board renders
// every week from these same point rows, so progress and scores left behind
// would belong to a point no week can show.
func TestReplaceKRCoreDropsWeeklyRowsOfRemovedPoints(t *testing.T) {
	db := openWorkspaceTestDB(t)
	objective := domain.Objective{ID: "o-drop-weekly", Title: "增长", Quarter: "2026-Q3"}
	kr := domain.KR{ID: "kr-drop-weekly", ObjectiveID: objective.ID, Title: "供给增长"}
	kept := domain.KRPoint{ID: "point-kept", KRID: kr.ID, Kind: domain.PointKindProduct, Title: "保留"}
	dropped := domain.KRPoint{ID: "point-dropped-weekly", KRID: kr.ID, Kind: domain.PointKindStrategy, Title: "移除"}
	for _, row := range []any{
		&objective, &kr, &kept, &dropped,
		&domain.KRProgress{ID: "pg-kept", PointID: kept.ID, Week: "2026-W35", Status: domain.StatusDone, Text: "保留的进展"},
		&domain.KRProgress{ID: "pg-drop-35", PointID: dropped.ID, Week: "2026-W35", Status: domain.StatusDone, Text: "上周"},
		&domain.KRProgress{ID: "pg-drop-36", PointID: dropped.ID, Week: "2026-W36", Status: domain.StatusDone, Text: "本周"},
		&domain.WeeklyScore{Quarter: objective.Quarter, Week: "2026-W36", TargetKind: domain.WeeklyScoreTargetPoint, TargetID: dropped.ID, Score: 0.8, UpdatedBy: "ou_editor"},
	} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	service, err := NewService(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceKRCore(t.Context(), kr.ID, ReplaceKRInput{
		ExpectedVersion: 0,
		Title:           kr.Title,
		Points:          []PointView{{ID: kept.ID, Kind: kept.Kind, Title: kept.Title, Tags: []TagView{}}},
	}); err != nil {
		t.Fatal(err)
	}
	var droppedProgress, droppedScores, droppedPoints, keptProgress int64
	if err := db.Model(&domain.KRProgress{}).Where("point_id = ?", dropped.ID).Count(&droppedProgress).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.WeeklyScore{}).Where("target_id = ?", dropped.ID).Count(&droppedScores).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KRPoint{}).Where("id = ?", dropped.ID).Count(&droppedPoints).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&domain.KRProgress{}).Where("point_id = ?", kept.ID).Count(&keptProgress).Error; err != nil {
		t.Fatal(err)
	}
	if droppedProgress != 0 || droppedScores != 0 || droppedPoints != 0 {
		t.Fatalf("removed point left rows behind: progress=%d scores=%d points=%d", droppedProgress, droppedScores, droppedPoints)
	}
	if keptProgress != 1 {
		t.Fatalf("removing one point disturbed another point's progress: count=%d", keptProgress)
	}
}
