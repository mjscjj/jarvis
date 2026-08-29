package okrworkspace

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type RegionSyncDraft struct {
	Quarter     string          `json:"quarter"`
	Week        string          `json:"week"`
	Region      string          `json:"region"`
	Mode        string          `json:"mode"`
	PublishMode string          `json:"publish_mode"`
	Summary     RegionSyncCount `json:"summary"`
	Rows        []RegionSyncRow `json:"rows"`
}

type RegionSyncCount struct {
	KRCount      int `json:"kr_count"`
	PointCount   int `json:"point_count"`
	RiskCount    int `json:"risk_count"`
	MissingCount int `json:"missing_count"`
}

type RegionSyncRow struct {
	ObjectiveID    string `json:"objective_id"`
	ObjectiveTitle string `json:"objective_title"`
	KRID           string `json:"kr_id"`
	KRTitle        string `json:"kr_title"`
	OwnerName      string `json:"owner_name"`
	PointID        string `json:"point_id"`
	PointTitle     string `json:"point_title"`
	Status         string `json:"status"`
	Detail         string `json:"detail"`
	Source         string `json:"source"`
	Risk           bool   `json:"risk"`
	Missing        bool   `json:"missing"`
}

// RegionSyncDraft builds a stable read-only projection from page facts. It
// neither polls external systems nor persists an editorial copy.
func (s *Service) RegionSyncDraft(ctx context.Context, quarter, week, region string) (RegionSyncDraft, error) {
	region = strings.TrimSpace(region)
	if region == "" {
		return RegionSyncDraft{}, fmt.Errorf("region is required")
	}
	board, err := s.Board(ctx, quarter, week)
	if err != nil {
		return RegionSyncDraft{}, err
	}
	result := RegionSyncDraft{
		Quarter: board.Quarter, Week: board.Week, Region: region,
		Mode: "preview_only", PublishMode: "unpublished", Rows: []RegionSyncRow{},
	}
	krSeen := map[string]struct{}{}
	for _, objective := range board.Objectives {
		for _, record := range objective.KRs {
			if !hasTag(record.Tags, "region", region) {
				continue
			}
			krSeen[record.ID] = struct{}{}
			for _, point := range record.Points {
				status := firstProgressStatus(point.Entries)
				detail := progressText(point.Entries)
				missing := detail == ""
				if missing {
					status, detail = "not_started", "本周尚未填写"
				}
				row := RegionSyncRow{
					ObjectiveID: objective.ID, ObjectiveTitle: objective.Title,
					KRID: record.ID, KRTitle: record.Title, OwnerName: displayOwner(record.OwnerName),
					PointID: point.ID, PointTitle: point.Title, Status: status, Detail: detail,
					Source: progressSource(point.Entries), Risk: isRiskStatus(status), Missing: missing,
				}
				result.Rows = append(result.Rows, row)
				if row.Risk {
					result.Summary.RiskCount++
				}
				if row.Missing {
					result.Summary.MissingCount++
				}
			}
		}
	}
	result.Summary.KRCount, result.Summary.PointCount = len(krSeen), len(result.Rows)
	sort.SliceStable(result.Rows, func(i, j int) bool {
		left, right := result.Rows[i], result.Rows[j]
		if left.Risk != right.Risk {
			return left.Risk
		}
		if left.Missing != right.Missing {
			return left.Missing
		}
		return left.ObjectiveID+"\x00"+left.KRID+"\x00"+left.PointID < right.ObjectiveID+"\x00"+right.KRID+"\x00"+right.PointID
	})
	return result, nil
}

func hasTag(tags []TagView, kind, value string) bool {
	for _, tag := range tags {
		if tag.Type == kind && tag.Value == value {
			return true
		}
	}
	return false
}

func progressText(entries []ProgressView) string {
	parts := make([]string, 0, len(entries))
	for _, entry := range entries {
		if text := strings.TrimSpace(entry.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "；")
}

func progressSource(entries []ProgressView) string {
	for _, entry := range entries {
		if strings.TrimSpace(entry.Text) != "" {
			return emptyAs(entry.Source, "page")
		}
	}
	return "page"
}
