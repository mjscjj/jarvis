package extract

import (
	"fmt"
	"testing"
)

func TestSnapshotConversationKeepsLatestTwentyFiveMessages(t *testing.T) {
	messages := make([]MessageContext, 30)
	for i := range messages {
		messages[i] = MessageContext{MessageID: fmt.Sprintf("om_%02d", i+1)}
	}

	got := snapshotConversation(ConversationUnit{Messages: messages})
	if len(got) != 25 {
		t.Fatalf("snapshot conversation length = %d, want 25", len(got))
	}
	if got[0].MessageID != "om_06" || got[len(got)-1].MessageID != "om_30" {
		t.Fatalf("snapshot conversation range = %s..%s, want om_06..om_30", got[0].MessageID, got[len(got)-1].MessageID)
	}
}
