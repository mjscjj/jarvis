package extract

import (
	"encoding/json"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"jarvis/internal/contextpack"
	"jarvis/internal/domain"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestM3EvidenceDoesNotCarryAdmission(t *testing.T) {
	c := strictCandidate()
	c.Payload = "UNVERIFIED_M3_PLAN"
	c.Annotation = json.RawMessage(`{"brief":"UNVERIFIED_M3_PLAN","scene":"UNVERIFIED_M3_PLAN","delegation_id":77}`)
	unit := ConversationUnit{Key: "chat", Messages: []MessageContext{{MessageID: "om_1", Content: c.SourceQuote, IsNew: true, Extractable: true, CreateTime: 1000}}}
	s := &PipelineStore{location: time.UTC}
	p, err := s.prepareCandidate(t.Context(), ChatBatch{Group: GroupContext{ChatID: "private", ChatMode: "p2p", PeerName: "真人"}}, unit, c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(p.Content), "UNVERIFIED_M3_PLAN") || !strings.Contains(string(p.Admission), "UNVERIFIED_M3_PLAN") {
		t.Fatal("incorrect admission ownership")
	}
	view, err := contextpack.Evidence(p.Content, "todo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(view), c.SourceQuote) || !strings.Contains(string(view), "77") {
		t.Fatalf("evidence or association lost %s", view)
	}
}
func TestReplyAncestorsSurviveAcrossDays(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "anchors.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&domain.Message{}); err != nil {
		t.Fatal(err)
	}
	root := domain.Message{MessageID: "root", ChatID: "chat", Content: "原始约定", CreateTime: 1, RenderOK: true}
	if err := db.Create(&root).Error; err != nil {
		t.Fatal(err)
	}
	store := &PipelineStore{db: db}
	rows, missing, err := store.includeAnchors(t.Context(), "chat", []MessageContext{{MessageID: "reply", ReplyTo: "root", RootID: "absent", Content: "这个继续", IsNew: true, CreateTime: 999999999}})
	if err != nil || len(rows) != 2 || len(missing) != 1 || !rows[0].IsAnchor {
		t.Fatalf("anchors %+v missing %v err %v", rows, missing, err)
	}
}

func TestPrivatePeerAndBotReplyAreInM3Scene(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "private.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(append(domain.CoreModels(), &domain.Message{})...); err != nil {
		t.Fatal(err)
	}
	chat := "private"
	peer := domain.Person{OpenID: "peer", Name: "张若怡", P2PChatID: &chat, Role: "key", IsActive: true}
	if err := db.Create(&peer).Error; err != nil {
		t.Fatal(err)
	}
	group := domain.Group{ChatID: chat, ChatMode: "p2p"}
	store := &PipelineStore{db: db, principalOpenID: "owner"}
	batch, err := store.buildChatBatch(t.Context(), &group, []domain.Message{
		{ID: 1, MessageID: "ask", ChatID: chat, SenderType: "user", SenderOpenID: "owner", Content: "你能看到么", RenderOK: true, CreateTime: 1000},
		{ID: 2, MessageID: "bot_reply", ChatID: chat, SenderType: "bot", SenderOpenID: "bot", Content: "已处理", RenderOK: true, CreateTime: 2000},
	}, LoadOptions{BatchMessages: 50, ContextMessages: 50, ContextWindow: 24 * time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if batch.Group.PeerName != "张若怡" || len(batch.Units) != 1 || len(batch.Units[0].Messages) != 2 || batch.Units[0].Messages[1].IsNew {
		t.Fatalf("incorrect scene %+v", batch)
	}
}
