package cardask

import (
	"encoding/json"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

func TestSourceButtonIsNavigationAndSurvivesAnswer(t *testing.T) {
	const link = "https://applink.feishu.cn/client/chat/open?openChatId=oc_group&position=42"
	for _, withInput := range []bool{false, true} {
		for _, outcome := range []string{"", "已回复"} {
			notice := testNotice()
			notice.Question.Fields = []execute.QuestionField{
				{Type: execute.FieldButton, Name: "go", Label: "继续"},
				{Type: execute.FieldButton, Name: "stop", Label: "停止"},
			}
			if withInput {
				notice.Question.Fields = append(notice.Question.Fields, execute.QuestionField{Type: execute.FieldInput, Name: "note", Label: "补充"})
			}
			notice.SourceURL = link
			card := questionCard(notice, "https://example.com/task", outcome)
			elements := card["body"].(map[string]any)["elements"].([]any)
			found := 0
			for _, item := range elements {
				element := item.(map[string]any)
				if element["tag"] == "form" {
					raw, _ := json.Marshal(element)
					if strings.Contains(string(raw), "消息原文") {
						t.Fatal("source link is part of the answer form")
					}
				}
				text, _ := element["text"].(map[string]any)
				if text["content"] != "消息原文" {
					continue
				}
				found++
				if element["tag"] != "button" || element["action_type"] != "link" {
					t.Fatalf("source control = %#v", element)
				}
				behaviors := element["behaviors"].([]any)
				behavior := behaviors[0].(map[string]any)
				if len(behaviors) != 1 || behavior["type"] != "open_url" || behavior["default_url"] != link {
					t.Fatalf("source behavior = %#v", behaviors)
				}
			}
			if found != 1 {
				t.Fatalf("source buttons = %d", found)
			}
			notice.SourceURL = ""
			raw, _ := json.Marshal(questionCard(notice, "https://example.com/task", outcome))
			if strings.Contains(string(raw), "消息原文") {
				t.Fatal("source button without a source URL")
			}
		}
	}
}
