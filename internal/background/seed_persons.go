package background

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/domain"
	"jarvis/internal/larkcli"

	"gorm.io/gorm"
)

// chatMemberLister is the lark-cli subset the person import needs, declared as
// an interface so the importer stays unit-testable.
type chatMemberLister interface {
	ListChatMembers(ctx context.Context, chatID string) ([]larkcli.ChatMember, error)
}

// PersonSeedStats reports what the group-member import created versus skipped.
type PersonSeedStats struct {
	GroupsScanned int
	PersonsSeen   int
	PersonsAdded  int
	PersonsSkip   int
}

// leaderOpenIDs are people the owner explicitly treats as leader. Anyone else
// imported from a key group defaults to colleague; the owner can promote them
// to key/leader from the UI. Keyed by open_id (member_id) so it survives
// renames. 严亮 is the owner's leader.
var leaderOpenIDs = map[string]bool{
	"ou_30a5a389ac7efb14e065483eb298aa3d": true, // 严亮
}

// roleDefaultWeightSeed mirrors the frontend role→weight defaults so imported
// persons get a sensible priority without manual entry.
var roleDefaultWeightSeed = map[string]float64{
	"leader": 1.0, "key": 0.7, "colleague": 0.4, "other": 0.1,
}

// SeedPersonsFromKeyGroups imports the human members of every is_key_group=1
// chat as Person rows, so the person list is populated with real people (real
// open_ids that M3 can match) instead of hand-typed or fake entries.
//
// It is idempotent: a member already present (by open_id) is skipped, never
// overwritten (so the owner's manual edits to relation/comm_style survive a
// re-run). Runs member fetch outside the DB tx and the inserts inside one tx;
// fails fast on any lark-cli or DB error.
func SeedPersonsFromKeyGroups(ctx context.Context, db *gorm.DB, lister chatMemberLister) (*PersonSeedStats, error) {
	if db == nil {
		return nil, fmt.Errorf("seed persons db is nil")
	}
	if lister == nil {
		return nil, fmt.Errorf("seed persons lister is nil")
	}

	var keyGroups []domain.Group
	if err := db.WithContext(ctx).
		Where("is_key_group = ? AND chat_id <> ''", true).
		Find(&keyGroups).Error; err != nil {
		return nil, fmt.Errorf("load key groups: %w", err)
	}

	// Collect the first-seen display name per unique open_id across all key
	// groups; later duplicates are ignored.
	nameByOpenID := make(map[string]string)
	stats := &PersonSeedStats{}
	for _, group := range keyGroups {
		members, err := lister.ListChatMembers(ctx, group.ChatID)
		if err != nil {
			return nil, fmt.Errorf("list members of key group %q: %w", group.ChatID, err)
		}
		stats.GroupsScanned++
		for _, m := range members {
			openID := strings.TrimSpace(m.MemberID)
			if openID == "" {
				continue
			}
			stats.PersonsSeen++
			if _, ok := nameByOpenID[openID]; !ok {
				nameByOpenID[openID] = strings.TrimSpace(m.Name)
			}
		}
	}
	if len(nameByOpenID) == 0 {
		return stats, nil
	}

	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for openID, memberName := range nameByOpenID {
			var existing domain.Person
			found := tx.Where("open_id = ?", openID).Limit(1).Find(&existing)
			if found.Error != nil {
				return fmt.Errorf("lookup person %q: %w", openID, found.Error)
			}
			if found.RowsAffected == 1 {
				stats.PersonsSkip++
				continue
			}
			role := "colleague"
			if leaderOpenIDs[openID] {
				role = "leader"
			}
			name := memberName
			if name == "" {
				name = openID
			}
			person := domain.Person{
				OpenID: openID, Name: name, Role: role,
				PriorityWeight: roleDefaultWeightSeed[role], IsActive: true,
			}
			if err := tx.Create(&person).Error; err != nil {
				return fmt.Errorf("create person %q: %w", openID, err)
			}
			stats.PersonsAdded++
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("seed persons from key groups: %w", err)
	}
	return stats, nil
}
