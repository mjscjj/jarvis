package okrworkspace

import (
	"context"
	"fmt"
	"gorm.io/gorm"
	"jarvis/internal/larkcli"
	"jarvis/internal/okrworkspace/domain"
)

type peopleDirectory interface {
	Resolve(context.Context, string) (larkcli.DirectoryPerson, error)
}

func (s *Service) SetPeopleDirectory(directory peopleDirectory) { s.people = directory }

func (s *Service) verifyPeople(ctx context.Context, people []domain.PersonRef) error {
	for i := range people {
		p := &people[i]
		p.Email = domain.NormalizeEmail(p.Email)
		if p.Email == "" {
			continue
		} // unresolved display-only owners
		if !domain.ValidEmail(p.Email) {
			return fmt.Errorf("人员邮箱无效，请刷新后重新选择")
		}
		if s.people == nil {
			continue
		} // standalone core stores verified caller data
		found, err := s.people.Resolve(ctx, p.Email)
		if err != nil {
			return fmt.Errorf("暂时无法核验人员 %s 的企业邮箱", p.Name)
		}
		if found.Name != p.Name || (p.UnionID != "" && found.UnionID != "" && p.UnionID != found.UnionID) {
			return fmt.Errorf("人员 %s 的姓名和邮箱不匹配，请重新选择", p.Name)
		}
		p.Email, p.Name, p.UnionID = found.Email, found.Name, found.UnionID
	}
	return nil
}

func (s *Service) verifyPlanPeople(ctx context.Context, objectives []PlanObjectiveView) error {
	for _, o := range objectives {
		if err := s.verifyPeople(ctx, o.Owners); err != nil {
			return err
		}
		for _, kr := range o.KRs {
			if err := s.verifyPeople(ctx, kr.Owners); err != nil {
				return err
			}
			for _, p := range kr.Points {
				if err := s.verifyPeople(ctx, p.Owners); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ValidatePersonIdentityMigration refuses to serve old IDs as empty emails.
func ValidatePersonIdentityMigration(db *gorm.DB) error {
	for _, table := range []string{"okr_workspace_objective_owner", "okr_workspace_kr_owner", "okr_workspace_point_owner"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var n int64
		if err := db.Table(table).Where("open_id <> ''").Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%s still contains app-local owner IDs; run scripts/okr-email-migrate before startup", table)
		}
	}
	for table, col := range map[string]string{"okr_workspace_comment": "mentions", "okr_workspace_follow_up": "owners", "okr_workspace_reminder_batch": "recipients_json"} {
		if !db.Migrator().HasTable(table) {
			continue
		}
		var n int64
		if err := db.Table(table).Where(col+" LIKE ? OR "+col+" LIKE ?", `%"open_id"%`, `%"owner_open_id"%`).Count(&n).Error; err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%s still contains app-local people IDs; run scripts/okr-email-migrate before startup", table)
		}
	}
	return nil
}
