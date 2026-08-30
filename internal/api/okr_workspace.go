package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"
	"jarvis/internal/observability"
	"jarvis/internal/okrworkspace"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"gorm.io/gorm"
)

type MarkdownDocumentCreator interface {
	CreateMarkdownDocument(ctx context.Context, title, content string) (larkcli.MarkdownDocument, error)
}

type createOKRDocumentRequest struct {
	Title   string `json:"title"`
	Content string `json:"content"`
}

func CreateOKRDocument(creator MarkdownDocumentCreator) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		if creator == nil {
			writeAPIError(c, consts.StatusServiceUnavailable, 50357, fmt.Errorf("lark document export is unavailable"))
			return
		}
		var request createOKRDocumentRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40057, err)
			return
		}
		result, err := creator.CreateMarkdownDocument(ctx, request.Title, request.Content)
		if err != nil {
			writeAPIError(c, consts.StatusBadGateway, 50257, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": map[string]any{
			"document_id": result.DocumentID,
			"url":         result.URL,
			"warnings":    result.Warnings,
		}})
	}
}

func writeAPIConflict(c *app.RequestContext, code int, err error, current any) {
	ctx := observability.FromRequestContext(context.Background(), c)
	hlog.CtxWarnf(ctx, "OKR workspace optimistic lock conflict code=%d error=%+v", code, err)
	c.JSON(consts.StatusConflict, map[string]any{"code": code, "msg": err.Error(), "logid": observability.LogID(ctx), "data": current})
}

func SearchWorkspacePeople(db *gorm.DB) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		query := strings.TrimSpace(c.Query("q"))
		if query == "" {
			writeAPIError(c, consts.StatusBadRequest, 40070, fmt.Errorf("q is required"))
			return
		}
		var rows []domain.Person
		like := "%" + query + "%"
		if err := db.WithContext(ctx).Where("is_active = ? AND (name LIKE ? OR open_id LIKE ?)", true, like, like).Order("name ASC").Limit(10).Find(&rows).Error; err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, err)
			return
		}
		users := make([]map[string]string, 0, len(rows))
		for _, row := range rows {
			department := ""
			if row.Department != nil {
				department = *row.Department
			}
			users = append(users, map[string]string{"open_id": row.OpenID, "name": row.Name, "department": department})
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]any{"users": users, "has_more": false}})
	}
}

func Enums() app.HandlerFunc {
	return func(_ context.Context, c *app.RequestContext) {
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": okrworkspace.EnumValues()})
	}
}

func GetOKRWorkspaceScope(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.LatestCoreScope(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusNotFound, 40411, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetWeeklyReportScope(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.LatestWeeklyScope(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusNotFound, 40412, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetBoard(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter := strings.TrimSpace(c.Query("quarter"))
		week := strings.TrimSpace(c.Query("week"))
		result, err := service.Board(ctx, quarter, week)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40010, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetCoreBoard(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.CoreBoard(ctx, strings.TrimSpace(c.Query("quarter")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40009, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetCoreKR(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("kr_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40013, fmt.Errorf("kr_id is required"))
			return
		}
		result, err := service.GetCoreKR(ctx, id)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40413, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40013, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetWeeklyReportWeeks(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter := strings.TrimSpace(c.Query("quarter"))
		if quarter == "" {
			scope, err := service.LatestCoreScope(ctx)
			if err != nil {
				writeAPIError(c, consts.StatusNotFound, 40414, err)
				return
			}
			quarter = scope.Quarter
		}
		result, err := service.ListWeeks(ctx, quarter)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40014, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func OpenWeeklyReportWeek(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.OpenWeekInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40015, err)
			return
		}
		input.OpenedBy = currentOKRIdentity(c).OpenID
		result, err := service.OpenWeek(ctx, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40015, err)
			return
		}
		status := consts.StatusOK
		if result.Created {
			status = consts.StatusCreated
		}
		c.JSON(status, map[string]any{"code": 0, "data": result})
	}
}

func GetComments(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.Comments(ctx, strings.TrimSpace(c.Query("quarter")), strings.TrimSpace(c.Query("week")))
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40060, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type createCommentRequest struct {
	Quarter         string `json:"quarter"`
	Week            string `json:"week"`
	ParentID        string `json:"parent_id"`
	TargetType      string `json:"target_type"`
	TargetID        string `json:"target_id"`
	TargetTitle     string `json:"target_title"`
	SelectedText    string `json:"selected_text"`
	SelectionStart  int    `json:"selection_start"`
	SelectionEnd    int    `json:"selection_end"`
	SelectionPrefix string `json:"selection_prefix"`
	SelectionSuffix string `json:"selection_suffix"`
	Content         string `json:"content"`
}

func CreateComment(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		identity := currentOKRIdentity(c)
		var request createCommentRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40061, err)
			return
		}
		input := okrworkspace.CreateCommentInput{
			Quarter: request.Quarter, Week: request.Week, ParentID: request.ParentID,
			TargetType: request.TargetType, TargetID: request.TargetID, TargetTitle: request.TargetTitle,
			SelectedText: request.SelectedText, SelectionStart: request.SelectionStart, SelectionEnd: request.SelectionEnd,
			SelectionPrefix: request.SelectionPrefix, SelectionSuffix: request.SelectionSuffix,
			AuthorOpenID: identity.OpenID, AuthorName: identity.Name, Content: request.Content,
		}
		result, err := service.CreateComment(ctx, input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40461, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40062, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func UpdateComment(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.UpdateCommentInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40063, err)
			return
		}
		result, err := service.UpdateComment(ctx, strings.TrimSpace(c.Param("comment_id")), input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40463, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40064, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteComment(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("comment_id"))
		if err := service.DeleteComment(ctx, id); errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40465, err)
			return
		} else if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40065, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": id}})
	}
}

func GetReminderPreview(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter := strings.TrimSpace(c.Query("quarter"))
		week := strings.TrimSpace(c.Query("week"))
		result, err := service.ReminderPreview(ctx, quarter, week)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40030, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

type generateReminderBatchInput struct {
	Quarter string `json:"quarter"`
	Week    string `json:"week"`
}

func GenerateReminderBatch(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input generateReminderBatchInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		result, err := service.GenerateReminderBatch(ctx, input.Quarter, input.Week, "manual")
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetReminderBatches(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		result, err := service.ReminderBatches(ctx, strings.TrimSpace(c.Query("quarter")), strings.TrimSpace(c.Query("week")), 12)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetMeegoPreview(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pointID := strings.TrimSpace(c.Param("point_id"))
		week := strings.TrimSpace(c.Query("week"))
		result, err := service.MeegoPreview(ctx, pointID, week)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40431, err)
			return
		}
		if errors.Is(err, okrworkspace.ErrMeegoUnavailable) {
			writeAPIError(c, consts.StatusServiceUnavailable, 50331, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func GetMeegoBatchPreview(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		quarter := strings.TrimSpace(c.Query("quarter"))
		week := strings.TrimSpace(c.Query("week"))
		result, err := service.MeegoBatchPreview(ctx, quarter, week)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40032, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func StoreMeegoObservation(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.MeegoObservationInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40036, err)
			return
		}
		result, err := service.StoreMeegoObservation(ctx, input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40436, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40036, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func ConfirmMeegoProgress(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pointID := strings.TrimSpace(c.Param("point_id"))
		if pointID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40033, fmt.Errorf("point_id is required"))
			return
		}
		var input okrworkspace.ConfirmMeegoProgressInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40034, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.ConfirmMeegoProgress(ctx, pointID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetKRByPoint(ctx, pointID, input.Week)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50035, currentErr)
				return
			}
			writeAPIConflict(c, 40935, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40435, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40035, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateWeeklyProgressEntry(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		pointID := strings.TrimSpace(c.Param("point_id"))
		if pointID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40037, fmt.Errorf("point_id is required"))
			return
		}
		var input okrworkspace.ProgressEntryInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40037, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.CreateProgressEntry(ctx, pointID, input)
		if writeProgressEntryError(ctx, c, service, pointID, input.Week, 37, err) {
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateWeeklyProgressEntry(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		progressID := strings.TrimSpace(c.Param("progress_id"))
		if progressID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40038, fmt.Errorf("progress_id is required"))
			return
		}
		var input okrworkspace.ProgressEntryInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40038, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.UpdateProgressEntry(ctx, progressID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetKRByProgress(ctx, progressID)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50038, currentErr)
				return
			}
			writeAPIConflict(c, 40938, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40438, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40038, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteWeeklyProgressEntry(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		progressID := strings.TrimSpace(c.Param("progress_id"))
		if progressID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40039, fmt.Errorf("progress_id is required"))
			return
		}
		var input okrworkspace.DeleteProgressEntryInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40039, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := service.DeleteProgressEntry(ctx, progressID, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			current, currentErr := service.GetKRByProgress(ctx, progressID)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50039, currentErr)
				return
			}
			writeAPIConflict(c, 40939, err, current)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40439, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40039, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func writeProgressEntryError(ctx context.Context, c *app.RequestContext, service *okrworkspace.Service, pointID, week string, suffix int, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, okrworkspace.ErrConflict) {
		current, currentErr := service.GetKRByPoint(ctx, pointID, week)
		if currentErr != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50000+suffix, currentErr)
			return true
		}
		writeAPIConflict(c, 40900+suffix, err, current)
		return true
	}
	if errors.Is(err, okrworkspace.ErrNotFound) {
		writeAPIError(c, consts.StatusNotFound, 40400+suffix, err)
		return true
	}
	writeAPIError(c, consts.StatusBadRequest, 40000+suffix, err)
	return true
}

func ReplaceCoreKR(service *okrworkspace.Service) app.HandlerFunc {
	return replaceKRWith(service.ReplaceKRCore, func(ctx context.Context, id string, _ okrworkspace.ReplaceKRInput) (okrworkspace.KRView, error) {
		return service.GetCoreKR(ctx, id)
	})
}

func replaceKRWith(replace func(context.Context, string, okrworkspace.ReplaceKRInput) (okrworkspace.KRView, error), current func(context.Context, string, okrworkspace.ReplaceKRInput) (okrworkspace.KRView, error)) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("kr_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40020, fmt.Errorf("kr_id is required"))
			return
		}
		var input okrworkspace.ReplaceKRInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40021, err)
			return
		}
		input.UpdatedBy = currentOKRIdentity(c).OpenID
		result, err := replace(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrConflict) {
			currentValue, currentErr := current(ctx, id, input)
			if currentErr != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50021, currentErr)
				return
			}
			writeAPIConflict(c, 40920, err, currentValue)
			return
		}
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40420, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40022, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func CreateObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var input okrworkspace.CreateObjectiveInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40027, err)
			return
		}
		result, err := service.CreateObjective(ctx, input)
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40028, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func UpdateObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("objective_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40029, fmt.Errorf("objective_id is required"))
			return
		}
		var input okrworkspace.UpdateObjectiveInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40030, err)
			return
		}
		result, err := service.UpdateObjective(ctx, id, input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40429, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40031, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}

func DeleteObjective(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("objective_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40032, fmt.Errorf("objective_id is required"))
			return
		}
		if err := service.DeleteObjective(ctx, id); errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40432, err)
			return
		} else if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40033, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": id}})
	}
}

func CreateKR(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		objectiveID := strings.TrimSpace(c.Param("objective_id"))
		if objectiveID == "" {
			writeAPIError(c, consts.StatusBadRequest, 40023, fmt.Errorf("objective_id is required"))
			return
		}
		var input okrworkspace.CreateKRInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40024, err)
			return
		}
		input.CreatedBy = currentOKRIdentity(c).OpenID
		result, err := service.CreateKR(ctx, objectiveID, input)
		if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40423, err)
			return
		}
		if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40025, err)
			return
		}
		c.JSON(consts.StatusCreated, map[string]any{"code": 0, "data": result})
	}
}

func DeleteKR(service *okrworkspace.Service) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		id := strings.TrimSpace(c.Param("kr_id"))
		if id == "" {
			writeAPIError(c, consts.StatusBadRequest, 40026, fmt.Errorf("kr_id is required"))
			return
		}
		var input okrworkspace.DeleteKRInput
		if err := decodeStrictJSON(c.Request.Body(), &input); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40027, err)
			return
		}
		if err := service.DeleteKR(ctx, id, input); errors.Is(err, okrworkspace.ErrConflict) {
			writeAPIConflict(c, 40926, err, map[string]string{"id": id})
			return
		} else if errors.Is(err, okrworkspace.ErrNotFound) {
			writeAPIError(c, consts.StatusNotFound, 40426, err)
			return
		} else if err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40028, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": map[string]string{"id": id}})
	}
}
