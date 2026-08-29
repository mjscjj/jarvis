package okrworkspace

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"jarvis/internal/okrworkspace/domain"

	"gorm.io/gorm"
)

const (
	ReportTypeMiddlePlatformWeekly = "middle_platform_weekly"
	ReportTypeBiweeklyReview       = "biweekly_review"
)

type GenerateReportDraftInput struct {
	Quarter    string `json:"quarter"`
	Week       string `json:"week"`
	ReportType string `json:"report_type"`
	TagType    string `json:"tag_type"`
	TagValue   string `json:"tag_value"`
}

type SaveReportDraftInput struct {
	GenerateReportDraftInput
	ExpectedVersion int32                       `json:"expected_version"`
	UpdatedBy       string                      `json:"updated_by"`
	Title           string                      `json:"title"`
	Sections        []domain.ReportDraftSection `json:"sections"`
}

type ReportDraftView struct {
	ID          string                      `json:"id"`
	Quarter     string                      `json:"quarter"`
	Week        string                      `json:"week"`
	ReportType  string                      `json:"report_type"`
	TagType     string                      `json:"tag_type"`
	TagValue    string                      `json:"tag_value"`
	Title       string                      `json:"title"`
	Sections    []domain.ReportDraftSection `json:"sections"`
	Version     int32                       `json:"version"`
	Saved       bool                        `json:"saved"`
	PublishMode string                      `json:"publish_mode"`
}

type RestoreReportDraftInput struct {
	ExpectedVersion int32  `json:"expected_version"`
	RevisionVersion int32  `json:"revision_version"`
	UpdatedBy       string `json:"updated_by"`
}

type ReportDraftHistoryView struct {
	DraftID        string                    `json:"draft_id"`
	CurrentVersion int32                     `json:"current_version"`
	PublishMode    string                    `json:"publish_mode"`
	Revisions      []ReportDraftRevisionView `json:"revisions"`
}

type ReportDraftRevisionView struct {
	Version          int32                       `json:"version"`
	Title            string                      `json:"title"`
	Sections         []domain.ReportDraftSection `json:"sections"`
	UpdatedBy        string                      `json:"updated_by"`
	CreatedAt        string                      `json:"created_at"`
	TitleChanged     bool                        `json:"title_changed"`
	ChangedItemCount int                         `json:"changed_item_count"`
	ChangedItems     []ReportDraftChangedItem    `json:"changed_items"`
}

type ReportDraftChangedItem struct {
	ID         string `json:"id"`
	PointID    string `json:"point_id"`
	PointTitle string `json:"point_title"`
	Before     string `json:"before"`
	After      string `json:"after"`
}

// GenerateReportDraft builds a deterministic projection and overlays an
// existing human-edited draft when present. It performs no writes and never
// invokes an external reader.
func (s *Service) GenerateReportDraft(ctx context.Context, input GenerateReportDraftInput) (ReportDraftView, error) {
	generated, err := s.buildReportDraft(ctx, input)
	if err != nil {
		return ReportDraftView{}, err
	}
	var saved domain.ReportDraft
	err = s.db.WithContext(ctx).First(&saved, "id = ?", generated.ID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return generated, nil
	}
	if err != nil {
		return ReportDraftView{}, fmt.Errorf("load report draft: %w", err)
	}
	return reportDraftFromModel(saved), nil
}

func (s *Service) GetReportDraft(ctx context.Context, id string) (ReportDraftView, error) {
	var record domain.ReportDraft
	if err := s.db.WithContext(ctx).First(&record, "id = ?", strings.TrimSpace(id)).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReportDraftView{}, ErrNotFound
		}
		return ReportDraftView{}, fmt.Errorf("get report draft: %w", err)
	}
	return reportDraftFromModel(record), nil
}

// SaveReportDraft stores a separate editorial projection with optimistic
// locking. Provenance is rebuilt from current KR facts and cannot be edited.
func (s *Service) SaveReportDraft(ctx context.Context, id string, input SaveReportDraftInput) (ReportDraftView, error) {
	id = strings.TrimSpace(id)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	input.Title = strings.TrimSpace(input.Title)
	if id == "" || input.ExpectedVersion < 0 || input.UpdatedBy == "" || input.Title == "" {
		return ReportDraftView{}, fmt.Errorf("draft id, expected_version, updater, and title are required")
	}
	canonical, err := s.buildReportDraft(ctx, input.GenerateReportDraftInput)
	if err != nil {
		return ReportDraftView{}, err
	}
	if canonical.ID != id {
		return ReportDraftView{}, fmt.Errorf("draft id does not match its reporting scope")
	}
	sections, err := mergeReportDraftEdits(canonical.Sections, input.Sections)
	if err != nil {
		return ReportDraftView{}, err
	}
	sectionsJSON, err := json.Marshal(sections)
	if err != nil {
		return ReportDraftView{}, fmt.Errorf("encode report draft sections: %w", err)
	}

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing domain.ReportDraft
		err := tx.First(&existing, "id = ?", id).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if input.ExpectedVersion != 0 {
				return ErrConflict
			}
			record := domain.ReportDraft{
				ID: id, Quarter: canonical.Quarter, Week: canonical.Week, ReportType: canonical.ReportType,
				TagType: canonical.TagType, TagValue: canonical.TagValue, Title: input.Title,
				Sections: sections, Version: 1, UpdatedBy: input.UpdatedBy,
			}
			if err := tx.Create(&record).Error; err != nil {
				return fmt.Errorf("create report draft: %w", err)
			}
			return createReportDraftRevision(tx, record.ID, record.Version, record.Title, record.Sections, record.UpdatedBy)
		}
		if err != nil {
			return fmt.Errorf("load report draft for save: %w", err)
		}
		if err := ensureReportDraftRevision(tx, existing); err != nil {
			return err
		}
		result := tx.Model(&domain.ReportDraft{}).Where("id = ? AND version = ?", id, input.ExpectedVersion).Updates(map[string]any{
			"title": input.Title, "sections": string(sectionsJSON), "updated_by": input.UpdatedBy, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return fmt.Errorf("update report draft: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrConflict
		}
		return createReportDraftRevision(tx, id, input.ExpectedVersion+1, input.Title, sections, input.UpdatedBy)
	})
	if err != nil {
		return ReportDraftView{}, err
	}
	var saved domain.ReportDraft
	if err := s.db.WithContext(ctx).First(&saved, "id = ?", id).Error; err != nil {
		return ReportDraftView{}, fmt.Errorf("read saved report draft: %w", err)
	}
	return reportDraftFromModel(saved), nil
}

func (s *Service) ReportDraftHistory(ctx context.Context, id string) (ReportDraftHistoryView, error) {
	current, err := s.GetReportDraft(ctx, id)
	if err != nil {
		return ReportDraftHistoryView{}, err
	}
	var records []domain.ReportDraftRevision
	if err := s.db.WithContext(ctx).Where("draft_id = ?", current.ID).Order("version DESC").Find(&records).Error; err != nil {
		return ReportDraftHistoryView{}, fmt.Errorf("list report draft revisions: %w", err)
	}
	byVersion := make(map[int32]domain.ReportDraftRevision, len(records))
	for _, record := range records {
		byVersion[record.Version] = record
	}
	result := ReportDraftHistoryView{DraftID: current.ID, CurrentVersion: current.Version, PublishMode: "unpublished", Revisions: []ReportDraftRevisionView{}}
	for _, record := range records {
		previous, hasPrevious := byVersion[record.Version-1]
		changed := reportDraftChanges(previous.Sections, record.Sections)
		result.Revisions = append(result.Revisions, ReportDraftRevisionView{
			Version: record.Version, Title: record.Title, Sections: nonNilReportSections(record.Sections),
			UpdatedBy: record.UpdatedBy, CreatedAt: record.CreatedAt.UTC().Format(time.RFC3339),
			TitleChanged:     hasPrevious && previous.Title != record.Title,
			ChangedItemCount: len(changed), ChangedItems: changed,
		})
	}
	return result, nil
}

func (s *Service) RestoreReportDraft(ctx context.Context, id string, input RestoreReportDraftInput) (ReportDraftView, error) {
	id = strings.TrimSpace(id)
	input.UpdatedBy = strings.TrimSpace(input.UpdatedBy)
	if id == "" || input.ExpectedVersion < 1 || input.RevisionVersion < 1 || input.UpdatedBy == "" {
		return ReportDraftView{}, fmt.Errorf("draft id, current expected_version, revision_version, and updater are required")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current domain.ReportDraft
		if err := tx.First(&current, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("load draft for restore: %w", err)
		}
		if err := ensureReportDraftRevision(tx, current); err != nil {
			return err
		}
		var target domain.ReportDraftRevision
		if err := tx.Where("draft_id = ? AND version = ?", id, input.RevisionVersion).First(&target).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("load restore revision: %w", err)
		}
		sectionsJSON, err := json.Marshal(target.Sections)
		if err != nil {
			return fmt.Errorf("encode restored sections: %w", err)
		}
		result := tx.Model(&domain.ReportDraft{}).Where("id = ? AND version = ?", id, input.ExpectedVersion).Updates(map[string]any{
			"title": target.Title, "sections": string(sectionsJSON), "updated_by": input.UpdatedBy, "version": gorm.Expr("version + 1"),
		})
		if result.Error != nil {
			return fmt.Errorf("restore report draft: %w", result.Error)
		}
		if result.RowsAffected == 0 {
			return ErrConflict
		}
		return createReportDraftRevision(tx, id, input.ExpectedVersion+1, target.Title, target.Sections, input.UpdatedBy)
	})
	if err != nil {
		return ReportDraftView{}, err
	}
	return s.GetReportDraft(ctx, id)
}

func ensureReportDraftRevision(tx *gorm.DB, record domain.ReportDraft) error {
	var count int64
	if err := tx.Model(&domain.ReportDraftRevision{}).Where("draft_id = ? AND version = ?", record.ID, record.Version).Count(&count).Error; err != nil {
		return fmt.Errorf("check report draft revision: %w", err)
	}
	if count > 0 {
		return nil
	}
	return createReportDraftRevision(tx, record.ID, record.Version, record.Title, record.Sections, record.UpdatedBy)
}

func createReportDraftRevision(tx *gorm.DB, draftID string, version int32, title string, sections []domain.ReportDraftSection, updatedBy string) error {
	revision := domain.ReportDraftRevision{
		ID: fmt.Sprintf("%s-v%d", draftID, version), DraftID: draftID, Version: version,
		Title: title, Sections: nonNilReportSections(sections), UpdatedBy: updatedBy,
	}
	if err := tx.Create(&revision).Error; err != nil {
		return fmt.Errorf("create report draft revision: %w", err)
	}
	return nil
}

func reportDraftChanges(beforeSections, afterSections []domain.ReportDraftSection) []ReportDraftChangedItem {
	before := reportDraftItemMap(beforeSections)
	after := reportDraftItemMap(afterSections)
	ids := map[string]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	result := []ReportDraftChangedItem{}
	for id := range ids {
		left, right := before[id], after[id]
		if left.Detail == right.Detail {
			continue
		}
		pointID, pointTitle := right.PointID, right.PointTitle
		if pointID == "" {
			pointID, pointTitle = left.PointID, left.PointTitle
		}
		result = append(result, ReportDraftChangedItem{ID: id, PointID: pointID, PointTitle: pointTitle, Before: left.Detail, After: right.Detail})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}

func reportDraftItemMap(sections []domain.ReportDraftSection) map[string]domain.ReportDraftItem {
	result := map[string]domain.ReportDraftItem{}
	for _, section := range sections {
		for _, item := range section.Items {
			result[item.ID] = item
		}
	}
	return result
}

func nonNilReportSections(sections []domain.ReportDraftSection) []domain.ReportDraftSection {
	if sections == nil {
		return []domain.ReportDraftSection{}
	}
	return sections
}

func (s *Service) buildReportDraft(ctx context.Context, input GenerateReportDraftInput) (ReportDraftView, error) {
	input.Quarter = strings.TrimSpace(input.Quarter)
	input.Week = strings.TrimSpace(input.Week)
	input.ReportType = strings.TrimSpace(input.ReportType)
	input.TagType = strings.TrimSpace(input.TagType)
	input.TagValue = strings.TrimSpace(input.TagValue)
	if input.Quarter == "" || !weekPattern.MatchString(input.Week) || !validReportType(input.ReportType) {
		return ReportDraftView{}, fmt.Errorf("quarter, YYYY-Www week, and a valid report_type are required")
	}
	if (input.TagType == "") != (input.TagValue == "") {
		return ReportDraftView{}, fmt.Errorf("tag_type and tag_value must be provided together")
	}
	board, err := s.Board(ctx, input.Quarter, input.Week)
	if err != nil {
		return ReportDraftView{}, err
	}
	titles := reportSectionTitles(input.ReportType)
	sections := []domain.ReportDraftSection{
		{Kind: "progress", Title: titles[0], Items: []domain.ReportDraftItem{}},
		{Kind: "risk", Title: titles[1], Items: []domain.ReportDraftItem{}},
		{Kind: "change", Title: titles[2], Items: []domain.ReportDraftItem{}},
	}
	for _, objective := range board.Objectives {
		for _, record := range objective.KRs {
			if !reportTagMatches(record.Tags, input.TagType, input.TagValue) {
				continue
			}
			for _, point := range record.Points {
				for _, entry := range point.Entries {
					if strings.TrimSpace(entry.Text) == "" {
						continue
					}
					item := reportDraftItem("progress:"+entry.ID, objective, record, point, input.Week, string(entry.Status), normalizedSource(entry.Source), entry.Text)
					sections[0].Items = append(sections[0].Items, item)
					if isRiskStatus(string(entry.Status)) {
						riskItem := item
						riskItem.ID = "risk:" + entry.ID
						sections[1].Items = append(sections[1].Items, riskItem)
					}
				}
				current, previous := progressDigest(point.Entries), progressDigest(point.PreviousEntries)
				if current != previous && (current != "" || previous != "") {
					detail := "上次：" + emptyAs(previous, "无") + " → 本周：" + emptyAs(current, "无")
					sections[2].Items = append(sections[2].Items, reportDraftItem("change:"+point.ID, objective, record, point, input.Week, firstProgressStatus(point.Entries), "page_history", detail))
				}
			}
		}
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{input.Quarter, input.Week, input.ReportType, input.TagType, input.TagValue}, "\x00")))
	return ReportDraftView{
		ID: fmt.Sprintf("report-%x", digest[:8]), Quarter: input.Quarter, Week: input.Week,
		ReportType: input.ReportType, TagType: input.TagType, TagValue: input.TagValue,
		Title: reportDraftTitle(input.ReportType, input.Week), Sections: sections,
		Version: 0, Saved: false, PublishMode: "unpublished",
	}, nil
}

func reportDraftItem(id string, objective ObjectiveView, record KRView, point PointView, week, status, source, detail string) domain.ReportDraftItem {
	return domain.ReportDraftItem{
		ID: id, ObjectiveID: objective.ID, ObjectiveTitle: objective.Title,
		KRID: record.ID, KRTitle: record.Title, OwnerName: displayOwner(record.OwnerName),
		PointID: point.ID, PointTitle: point.Title, Week: week,
		Status: status, Source: source, Detail: strings.TrimSpace(detail),
	}
}

func mergeReportDraftEdits(canonical, edited []domain.ReportDraftSection) ([]domain.ReportDraftSection, error) {
	edits := map[string]string{}
	valid := map[string]bool{}
	for _, section := range canonical {
		for _, item := range section.Items {
			valid[item.ID] = true
		}
	}
	for _, section := range edited {
		for _, item := range section.Items {
			if !valid[item.ID] {
				return nil, fmt.Errorf("draft item %q is not part of the current reporting scope", item.ID)
			}
			edits[item.ID] = item.Detail
		}
	}
	for sectionIndex := range canonical {
		for itemIndex := range canonical[sectionIndex].Items {
			item := &canonical[sectionIndex].Items[itemIndex]
			if detail, ok := edits[item.ID]; ok {
				item.Detail = detail
			}
		}
	}
	return canonical, nil
}

func reportDraftFromModel(record domain.ReportDraft) ReportDraftView {
	sections := record.Sections
	if sections == nil {
		sections = []domain.ReportDraftSection{}
	}
	return ReportDraftView{
		ID: record.ID, Quarter: record.Quarter, Week: record.Week, ReportType: record.ReportType,
		TagType: record.TagType, TagValue: record.TagValue, Title: record.Title,
		Sections: sections, Version: record.Version, Saved: true, PublishMode: "unpublished",
	}
}

func validReportType(value string) bool {
	return value == ReportTypeMiddlePlatformWeekly || value == ReportTypeBiweeklyReview
}

func reportTagMatches(tags []TagView, tagType, tagValue string) bool {
	if tagType == "" {
		return true
	}
	for _, tag := range tags {
		if tag.Type == tagType && tag.Value == tagValue {
			return true
		}
	}
	return false
}

func reportSectionTitles(reportType string) [3]string {
	if reportType == ReportTypeBiweeklyReview {
		return [3]string{"阶段进展", "风险与决策", "阶段变化"}
	}
	return [3]string{"本周进展", "风险与协同", "较上周变化"}
}

func reportDraftTitle(reportType, week string) string {
	if reportType == ReportTypeBiweeklyReview {
		return week + " 双周会材料草稿"
	}
	return week + " 中台周报草稿"
}
