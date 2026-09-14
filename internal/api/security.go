package api

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"jarvis/internal/config"
	"jarvis/internal/security"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type SecuritySettingsService interface {
	GetSecurity(context.Context) (*config.SecuritySettingsView, error)
	UpdateSecurity(context.Context, config.SecuritySettings) (*config.SecuritySettingsView, error)
}

type p2pCheckpointAdvancer interface {
	AdvanceP2PCheckpoints(context.Context) error
	AdvanceAutomaticP2PCheckpoints(context.Context) error
}

type securitySettingsRequest struct {
	P2PScanEnabled     *bool `json:"p2p_scan_enabled"`
	AutoRelatedP2PTopN *int  `json:"auto_related_p2p_top_n"`
}

func GetSecuritySettings(service SecuritySettingsService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := service.GetSecurity(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50092, fmt.Errorf("get security settings failed: %w", err))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func UpdateSecuritySettings(service SecuritySettingsService, captureService p2pCheckpointAdvancer) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request securitySettingsRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40092, err)
			return
		}
		current, err := service.GetSecurity(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50092, fmt.Errorf("get security settings before save failed: %w", err))
			return
		}
		input := current.Settings
		if request.P2PScanEnabled != nil {
			input.P2PScanEnabled = *request.P2PScanEnabled
		}
		if request.AutoRelatedP2PTopN != nil {
			input.AutoRelatedP2PTopN = *request.AutoRelatedP2PTopN
		}
		if input.P2PScanEnabled && !current.Settings.P2PScanEnabled {
			if captureService == nil {
				writeAPIError(c, consts.StatusServiceUnavailable, 50392, fmt.Errorf("capture service is unavailable"))
				return
			}
			if err := captureService.AdvanceP2PCheckpoints(ctx); err != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50092, fmt.Errorf("advance p2p checkpoints before enabling scans failed: %w", err))
				return
			}
		}
		if input.AutoRelatedP2PTopN > 0 && current.Settings.AutoRelatedP2PTopN == 0 {
			if captureService == nil {
				writeAPIError(c, consts.StatusServiceUnavailable, 50392, fmt.Errorf("capture service is unavailable"))
				return
			}
			if err := captureService.AdvanceAutomaticP2PCheckpoints(ctx); err != nil {
				writeAPIError(c, consts.StatusInternalServerError, 50092, fmt.Errorf("advance automatic p2p checkpoints before enabling scans failed: %w", err))
				return
			}
		}
		view, err := service.UpdateSecurity(ctx, input)
		if err != nil {
			if errors.Is(err, config.ErrInvalidRuntimeSettings) {
				writeAPIError(c, consts.StatusBadRequest, 40093, err)
				return
			}
			writeAPIError(c, consts.StatusInternalServerError, 50092, fmt.Errorf("save security settings failed: %w", err))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

func ListSecurityAuditEvents(service *security.AuditService) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		days := 7
		if raw := strings.TrimSpace(c.Query("days")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || (value != 1 && value != 7 && value != 30) {
				writeAPIError(c, consts.StatusBadRequest, 40094, fmt.Errorf("days must be 1, 7 or 30"))
				return
			}
			days = value
		}
		limit := 200
		if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || value > 500 {
				writeAPIError(c, consts.StatusBadRequest, 40094, fmt.Errorf("limit must be between 1 and 500"))
				return
			}
			limit = value
		}
		result, err := service.List(ctx, security.AuditFilter{
			Since:     time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour),
			ActorKind: strings.TrimSpace(c.Query("actor_kind")),
			Operation: strings.TrimSpace(c.Query("operation")),
			Route:     strings.TrimSpace(c.Query("route")),
			Resource:  strings.TrimSpace(c.Query("resource")),
			Limit:     limit,
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50093, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": result})
	}
}
