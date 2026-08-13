package background

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// ProfileInput is the editable principal ("me") background maintained from the
// admin UI. open_id is fixed by config, so it is not part of the input.
type ProfileInput struct {
	Name         string  `json:"name"`
	Department   *string `json:"department"`
	Title        *string `json:"title"`
	Background   *string `json:"background"`
	Preferences  *string `json:"preferences"`
	LeaderOpenID *string `json:"leader_open_id"`
	LeaderName   *string `json:"leader_name"`
}

func (in ProfileInput) validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("profile name must not be blank")
	}
	return nil
}

// ProfileView is the API projection of the principal profile, always carrying
// the configured open_id even before the row is first saved.
type ProfileView struct {
	ID           uint64  `json:"id"`
	OpenID       string  `json:"open_id"`
	Name         string  `json:"name"`
	Department   *string `json:"department"`
	Title        *string `json:"title"`
	Background   *string `json:"background"`
	Preferences  *string `json:"preferences"`
	LeaderOpenID *string `json:"leader_open_id"`
	LeaderName   *string `json:"leader_name"`
	Saved        bool    `json:"saved"`
}

// ProfileService owns the single-row principal_profile table. The principal
// open_id is authoritative from config; the service always reads/writes that one
// identity so there can only ever be one principal profile.
type ProfileService struct {
	db              *gorm.DB
	principalOpenID string
}

func NewProfileService(db *gorm.DB, principalOpenID string) (*ProfileService, error) {
	if db == nil {
		return nil, fmt.Errorf("profile service db is nil")
	}
	if strings.TrimSpace(principalOpenID) == "" {
		return nil, fmt.Errorf("profile service principal open_id is empty")
	}
	return &ProfileService{db: db, principalOpenID: principalOpenID}, nil
}

// Get returns the saved profile, or an unsaved view carrying the configured
// open_id so the UI can prompt the user to fill it in the first time.
func (s *ProfileService) Get(ctx context.Context) (*ProfileView, error) {
	var profile domain.PrincipalProfile
	err := s.db.WithContext(ctx).Where("open_id = ?", s.principalOpenID).Take(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &ProfileView{OpenID: s.principalOpenID, Saved: false}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get principal profile: %w", err)
	}
	view := toProfileView(&profile)
	return &view, nil
}

// Upsert writes the single principal profile for the configured open_id.
func (s *ProfileService) Upsert(ctx context.Context, in ProfileInput) (*ProfileView, error) {
	if err := in.validate(); err != nil {
		return nil, invalid(err)
	}
	var existing domain.PrincipalProfile
	found := s.db.WithContext(ctx).Where("open_id = ?", s.principalOpenID).Limit(1).Find(&existing)
	if found.Error != nil {
		return nil, fmt.Errorf("lookup principal profile: %w", found.Error)
	}
	if found.RowsAffected == 1 {
		updates := map[string]any{
			"name": in.Name, "department": in.Department, "title": in.Title,
			"background": in.Background, "preferences": in.Preferences,
			"leader_open_id": in.LeaderOpenID, "leader_name": in.LeaderName,
		}
		if err := s.db.WithContext(ctx).Model(&domain.PrincipalProfile{}).
			Where("open_id = ?", s.principalOpenID).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("update principal profile: %w", err)
		}
	} else {
		profile := domain.PrincipalProfile{
			OpenID: s.principalOpenID, Name: in.Name, Department: in.Department, Title: in.Title,
			Background: in.Background, Preferences: in.Preferences,
			LeaderOpenID: in.LeaderOpenID, LeaderName: in.LeaderName,
		}
		if err := s.db.WithContext(ctx).Create(&profile).Error; err != nil {
			return nil, fmt.Errorf("create principal profile: %w", err)
		}
	}
	return s.Get(ctx)
}

func toProfileView(profile *domain.PrincipalProfile) ProfileView {
	return ProfileView{
		ID: profile.ID, OpenID: profile.OpenID, Name: profile.Name, Department: profile.Department, Title: profile.Title,
		Background: profile.Background, Preferences: profile.Preferences,
		LeaderOpenID: profile.LeaderOpenID, LeaderName: profile.LeaderName, Saved: true,
	}
}
