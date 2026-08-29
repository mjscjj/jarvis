package okrworkspace

import (
	"context"
	"sort"
	"strings"
)

type QuarterlyOKRDraft struct {
	Quarter     string               `json:"quarter"`
	Week        string               `json:"week"`
	Mode        string               `json:"mode"`
	PublishMode string               `json:"publish_mode"`
	Summary     QuarterlyOKRSummary  `json:"summary"`
	Objectives  []QuarterlyObjective `json:"objectives"`
}

type QuarterlyOKRSummary struct {
	ObjectiveCount int `json:"objective_count"`
	KRCount        int `json:"kr_count"`
	RiskCount      int `json:"risk_count"`
	MissingCount   int `json:"missing_count"`
}

type QuarterlyObjective struct {
	ID    string        `json:"id"`
	Title string        `json:"title"`
	KRs   []QuarterlyKR `json:"krs"`
}

type QuarterlyKR struct {
	ID          string           `json:"id"`
	Title       string           `json:"title"`
	OwnerName   string           `json:"owner_name"`
	Priority    string           `json:"priority"`
	Status      string           `json:"status"`
	Risk        bool             `json:"risk"`
	Missing     bool             `json:"missing"`
	Metrics     []string         `json:"metrics"`
	KeyProgress string           `json:"key_progress"`
	Points      []QuarterlyPoint `json:"points"`
}

type QuarterlyPoint struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Status  string `json:"status"`
	Detail  string `json:"detail"`
	Source  string `json:"source"`
	Risk    bool   `json:"risk"`
	Missing bool   `json:"missing"`
}

// QuarterlyOKRDraft derives both team-detail and management-summary material
// from the same read-only projection. The client only changes density.
func (s *Service) QuarterlyOKRDraft(ctx context.Context, quarter, week string) (QuarterlyOKRDraft, error) {
	board, err := s.Board(ctx, quarter, week)
	if err != nil {
		return QuarterlyOKRDraft{}, err
	}
	result := QuarterlyOKRDraft{
		Quarter: board.Quarter, Week: board.Week, Mode: "preview_only", PublishMode: "unpublished",
		Objectives: []QuarterlyObjective{},
	}
	for _, objective := range board.Objectives {
		objectiveView := QuarterlyObjective{ID: objective.ID, Title: objective.Title, KRs: []QuarterlyKR{}}
		for _, record := range objective.KRs {
			item := QuarterlyKR{
				ID: record.ID, Title: record.Title, OwnerName: displayOwner(record.OwnerName), Priority: record.Priority,
				Metrics: []string{}, Points: []QuarterlyPoint{},
			}
			for _, metric := range record.Metrics {
				if text := strings.TrimSpace(metric.Text); text != "" {
					item.Metrics = append(item.Metrics, text)
				}
			}
			for _, point := range record.Points {
				status := firstProgressStatus(point.Entries)
				detail := progressText(point.Entries)
				missing := detail == ""
				if missing {
					status, detail = "not_started", "本周尚未填写"
				}
				pointView := QuarterlyPoint{ID: point.ID, Title: point.Title, Status: status, Detail: detail, Source: progressSource(point.Entries), Risk: isRiskStatus(status), Missing: missing}
				item.Points = append(item.Points, pointView)
				item.Risk = item.Risk || pointView.Risk
				item.Missing = item.Missing || pointView.Missing
			}
			item.Status = quarterlyKRStatus(item.Points)
			item.KeyProgress = quarterlyKeyProgress(item.Points)
			objectiveView.KRs = append(objectiveView.KRs, item)
			result.Summary.KRCount++
			if item.Risk {
				result.Summary.RiskCount++
			}
			if item.Missing {
				result.Summary.MissingCount++
			}
		}
		sort.SliceStable(objectiveView.KRs, func(i, j int) bool {
			left, right := objectiveView.KRs[i], objectiveView.KRs[j]
			if left.Risk != right.Risk {
				return left.Risk
			}
			if left.Missing != right.Missing {
				return left.Missing
			}
			return left.ID < right.ID
		})
		result.Objectives = append(result.Objectives, objectiveView)
	}
	result.Summary.ObjectiveCount = len(result.Objectives)
	return result, nil
}

func quarterlyKRStatus(points []QuarterlyPoint) string {
	for _, wanted := range []string{"blocked", "delayed", "at_risk", "in_progress", "not_started", "done"} {
		for _, point := range points {
			if point.Status == wanted {
				return wanted
			}
		}
	}
	return "not_started"
}

func quarterlyKeyProgress(points []QuarterlyPoint) string {
	for _, point := range points {
		if point.Risk {
			return point.Title + "：" + point.Detail
		}
	}
	for _, point := range points {
		if !point.Missing {
			return point.Title + "：" + point.Detail
		}
	}
	return "本周尚未填写"
}
