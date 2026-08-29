package okrworkspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	workspaceDomain "jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

// ImportResult describes the one-time move from Emily's standalone SQLite
// file. Existing Jarvis workspace rows are never overwritten.
type ImportResult struct {
	Imported bool
	Source   string
	Reason   string
}

// ImportLegacyIfEmpty copies the current Emily data into Jarvis's own SQLite
// database. The ATTACH exists only for this transaction; after import there is
// one runtime and one database.
func ImportLegacyIfEmpty(ctx context.Context, db *gorm.DB, source string) (ImportResult, error) {
	if db == nil {
		return ImportResult{}, fmt.Errorf("import Emily workspace: db is nil")
	}
	var count int64
	if err := db.WithContext(ctx).Model(&workspaceDomain.Objective{}).Count(&count).Error; err != nil {
		return ImportResult{}, fmt.Errorf("count existing OKR workspace objectives: %w", err)
	}
	if count > 0 {
		return ImportResult{Reason: "workspace already contains data"}, nil
	}
	absolute, err := filepath.Abs(source)
	if err != nil {
		return ImportResult{}, fmt.Errorf("resolve Emily database path: %w", err)
	}
	if _, err := os.Stat(absolute); err != nil {
		if os.IsNotExist(err) {
			if err := ensureFallbackDemo(ctx, db); err != nil {
				return ImportResult{}, err
			}
			return ImportResult{Imported: true, Reason: "legacy database absent; installed built-in 8/18 and 8/25 demo"}, nil
		}
		return ImportResult{}, fmt.Errorf("inspect Emily database %q: %w", absolute, err)
	}

	copyStatements := []string{
		`INSERT INTO okr_workspace_objective (id,title,quarter,sort_order,created_at,updated_at)
		 SELECT id,title,quarter,sort_order,created_at,updated_at FROM legacy_emily.objective`,
		`INSERT INTO okr_workspace_kr (id,objective_id,title,owner_open_id,owner_name,priority,metric_note,sort_order,version,created_by,updated_by,created_at,updated_at)
		 SELECT id,objective_id,title,owner_open_id,owner_name,priority,metric_note,sort_order,version,created_by,updated_by,created_at,updated_at FROM legacy_emily.kr`,
		`INSERT INTO okr_workspace_metric (id,kr_id,text,light,images,sort_order)
		 SELECT id,kr_id,text,light,COALESCE(images,'[]'),sort_order FROM legacy_emily.kr_metric`,
		`INSERT INTO okr_workspace_point (id,kr_id,kind,title,meego_work_item_id,meego_url,sort_order)
		 SELECT id,kr_id,kind,title,meego_work_item_id,meego_url,sort_order FROM legacy_emily.kr_point`,
		`INSERT INTO okr_workspace_progress (id,point_id,week,status,text,docs,images,source,needs_review,sort_order,created_by,updated_by,created_at,updated_at)
		 SELECT id,point_id,week,status,text,COALESCE(docs,'[]'),COALESCE(images,'[]'),source,needs_review,sort_order,created_by,updated_by,created_at,updated_at FROM legacy_emily.kr_progress`,
		`INSERT INTO okr_workspace_tag (kr_id,type,value) SELECT kr_id,type,value FROM legacy_emily.kr_tag`,
		`INSERT INTO okr_workspace_comment (id,quarter,week,parent_id,target_type,target_id,target_title,selected_text,selection_start,selection_end,selection_prefix,selection_suffix,author_open_id,author_name,content,created_at,updated_at)
		 SELECT id,quarter,week,parent_id,target_type,target_id,target_title,selected_text,selection_start,selection_end,selection_prefix,selection_suffix,author_open_id,author_name,content,created_at,updated_at FROM legacy_emily.page_comment`,
		`INSERT INTO okr_workspace_meego_snapshot (point_id,work_item_id,week,local_status,local_progress,remote_title,remote_status,remote_progress,remote_updated_at,status_changed,progress_changed,needs_review,risk,last_attempt_at,last_success_at,last_error,created_at,updated_at)
		 SELECT point_id,work_item_id,week,local_status,local_progress,remote_title,remote_status,remote_progress,remote_updated_at,status_changed,progress_changed,needs_review,risk,last_attempt_at,last_success_at,last_error,created_at,updated_at FROM legacy_emily.meego_sync_snapshot`,
		`INSERT INTO okr_workspace_report_draft (id,quarter,week,report_type,tag_type,tag_value,title,sections,version,updated_by,created_at,updated_at)
		 SELECT id,quarter,week,report_type,tag_type,tag_value,title,sections,version,updated_by,created_at,updated_at FROM legacy_emily.report_draft`,
		`INSERT INTO okr_workspace_report_revision (id,draft_id,version,title,sections,updated_by,created_at)
		 SELECT id,draft_id,version,title,sections,updated_by,created_at FROM legacy_emily.report_draft_revision`,
		`INSERT INTO okr_workspace_reminder_batch (id,quarter,week,trigger,status,recipient_count,missing_count,summary_json,recipients_json,last_error,started_at,finished_at,created_at,updated_at)
		 SELECT id,quarter,week,trigger,status,recipient_count,missing_count,summary_json,recipients_json,last_error,started_at,finished_at,created_at,updated_at FROM legacy_emily.reminder_batch`,
	}
	if err := db.WithContext(ctx).Exec("ATTACH DATABASE ? AS legacy_emily", absolute).Error; err != nil {
		return ImportResult{}, fmt.Errorf("attach Emily database: %w", err)
	}
	err = db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, statement := range copyStatements {
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("copy Emily workspace row set: %w", err)
			}
		}
		return nil
	})
	detachErr := db.WithContext(ctx).Exec("DETACH DATABASE legacy_emily").Error
	if err != nil {
		return ImportResult{}, err
	}
	if detachErr != nil {
		return ImportResult{}, fmt.Errorf("detach Emily database after import: %w", detachErr)
	}
	return ImportResult{Imported: true, Source: absolute}, nil
}

// ensureFallbackDemo keeps a fresh Jarvis checkout useful without carrying a
// second database file. The real migration path above copies the complete
// current Emily dataset.
func ensureFallbackDemo(ctx context.Context, db *gorm.DB) error {
	now := time.Now().UTC()
	objective := workspaceDomain.Objective{ID: "ws-o01", Title: "O1：B端市场", Quarter: "2026-Q3", CreatedAt: now, UpdatedAt: now}
	if err := db.WithContext(ctx).Create(&objective).Error; err != nil {
		return fmt.Errorf("create built-in OKR demo objective: %w", err)
	}
	kr := workspaceDomain.KR{
		ID: "ws-o01-k01", ObjectiveID: objective.ID,
		Title:     "KR1：公会大会顺利落地，参会率和满意度达成预期",
		OwnerName: "刘强、董旭", Priority: "p0", MetricNote: "8 月 25 日 · 较 8 月 18 日",
		CreatedBy: "jarvis", UpdatedBy: "jarvis", CreatedAt: now, UpdatedAt: now,
	}
	if err := db.WithContext(ctx).Create(&kr).Error; err != nil {
		return fmt.Errorf("create built-in OKR demo KR: %w", err)
	}
	metrics := []workspaceDomain.KRMetric{
		{ID: "ws-o01-k01-m1", KRID: kr.ID, Text: "核心邀约嘉宾 RSVP：0% → 51.5%（450 人），距 80% 目标 -28.5pp", Light: workspaceDomain.LightYellow},
		{ID: "ws-o01-k01-m2", KRID: kr.ID, Text: "大会宣发物料语种：0 → 23 种，预计 8/31 前完成上线", Light: workspaceDomain.LightGreen, SortOrder: 1},
		{ID: "ws-o01-k01-m3", KRID: kr.ID, Text: "团播 Tour 报名人数：0 → 229 人，目标 80 人（完成 286%）", Light: workspaceDomain.LightGreen, SortOrder: 2},
	}
	if err := db.WithContext(ctx).Create(&metrics).Error; err != nil {
		return fmt.Errorf("create built-in OKR demo metrics: %w", err)
	}
	point := workspaceDomain.KRPoint{ID: "ws-o01-k01-p01", KRID: kr.ID, Kind: workspaceDomain.PointKindStrategy, Title: "公会大会和颁奖典礼顺利落地，核心邀约嘉宾参会率 80%，整体满意度 ≥4 / 5"}
	if err := db.WithContext(ctx).Create(&point).Error; err != nil {
		return fmt.Errorf("create built-in OKR demo point: %w", err)
	}
	progress := []workspaceDomain.KRProgress{
		{ID: "ws-o01-k01-p01-w34", PointID: point.ID, Week: "2026-W34", Status: workspaceDomain.StatusInProgress, Text: "首轮邀约启动，RSVP 确认率 0%", Source: "weekly_document", CreatedBy: "import", UpdatedBy: "import", CreatedAt: now, UpdatedAt: now},
		{ID: "ws-o01-k01-p01-w35", PointID: point.ID, Week: "2026-W35", Status: workspaceDomain.StatusInProgress, Text: "RSVP 确认率达到 51.5%（450 人），继续推进补邀", Source: "weekly_document", CreatedBy: "import", UpdatedBy: "import", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.WithContext(ctx).Create(&progress).Error; err != nil {
		return fmt.Errorf("create built-in OKR demo progress: %w", err)
	}
	return nil
}
