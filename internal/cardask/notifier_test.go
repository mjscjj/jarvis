package cardask

import (
	"context"
	"encoding/json"
	"net"
	"strings"
	"testing"

	"jarvis/internal/execute"
)

type fakeLark struct {
	args     []string
	response any
	err      error
}

func (f *fakeLark) Run(_ context.Context, out any, args ...string) error {
	f.args = args
	if f.err != nil {
		return f.err
	}
	if out != nil && f.response != nil {
		encoded, err := json.Marshal(f.response)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, out)
	}
	return nil
}

func testNotifier(t *testing.T, lark larkRunner) *Notifier {
	t.Helper()
	notifier, err := newNotifier(lark, "Jarvis", "ou_principal", "127.0.0.1:18800", func() (net.IP, error) {
		return net.ParseIP("192.168.1.20"), nil
	})
	if err != nil {
		t.Fatalf("newNotifier() error = %v", err)
	}
	return notifier
}

func testNotice() execute.QuestionNotification {
	return execute.QuestionNotification{
		TaskID: 7, RunID: 21, Version: 12,
		TaskTitle: "评测结论同步", Summary: "已核对三份数据",
		Question: execute.Question{
			Title: "要把结论发到评测群吗",
			Body:  "拟发送：本轮评测通过率 92%。",
			Fields: []execute.QuestionField{
				{Type: execute.FieldButton, Name: "send", Label: "发出去", Style: "primary"},
				{Type: execute.FieldButton, Name: "skip", Label: "先不发", Style: "danger"},
				{Type: execute.FieldSelect, Name: "chat", Label: "发到哪个群", Options: []string{"评测群", "研发群"}},
				{Type: execute.FieldMultiSelect, Name: "cc", Label: "抄送", Options: []string{"A", "B"}},
				{Type: execute.FieldInput, Name: "note", Label: "补充说明"},
				{Type: execute.FieldLink, Name: "detail", Label: "看原始数据", URL: "https://example.com/report"},
			},
		},
	}
}

// TestSendQuestionKeepsComplexControlsInOneForm pins the layout contract:
// Feishu returns a form's inputs only when the submit button is a direct child
// of that form and carries no behaviors.
func TestSendQuestionKeepsComplexControlsInOneForm(t *testing.T) {
	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	delivery, err := testNotifier(t, lark).SendQuestion(context.Background(), testNotice())
	if err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}
	if delivery.MessageID != "om_card" || !strings.Contains(delivery.URL, "192.168.1.20:18800") {
		t.Fatalf("delivery = %#v", delivery)
	}

	card := sentCard(t, lark)
	form := onlyForm(t, card)
	elements, _ := form["elements"].([]any)
	kinds := map[string]int{}
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		kinds[element["tag"].(string)]++
	}
	if kinds["button"] != 3 || kinds["select_static"] != 1 || kinds["multi_select_static"] != 1 || kinds["input"] != 1 {
		t.Fatalf("rendered controls = %#v", kinds)
	}
	submits := 0
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		if element["tag"] != "button" || element["action_type"] != "form_submit" {
			continue
		}
		submits++
		if _, hasBehaviors := element["behaviors"]; hasBehaviors {
			t.Fatalf("submit button must carry no behaviors: %#v", element)
		}
		value, _ := element["value"].(map[string]any)
		if value["action"] != callbackAction || value["task_id"] != float64(7) || value["version"] != float64(12) {
			t.Fatalf("submit value = %#v", value)
		}
		if value["clicked"] != element["name"] {
			t.Fatalf("submit button %v must answer under its own name: %#v", element["name"], value)
		}
	}
	if submits != 2 {
		t.Fatalf("want 2 direct submit buttons, got %d: %#v", submits, elements)
	}
}

func TestSendQuestionRendersTwoButtonDecisionInOneCompactRow(t *testing.T) {
	notice := testNotice()
	notice.Question.Fields = []execute.QuestionField{
		{Type: execute.FieldButton, Name: "confirm", Label: "确认", Style: "primary"},
		{Type: execute.FieldButton, Name: "reject", Label: "拒绝", Style: "danger"},
	}
	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), notice); err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}

	elements := cardElements(t, sentCard(t, lark))
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		if element["tag"] == "form" {
			t.Fatalf("plain decision must not use a form: %#v", elements)
		}
	}
	row := onlyElementWithTag(t, elements, "column_set")
	if row["flex_mode"] != "bisect" {
		t.Fatalf("decision row flex_mode = %v", row["flex_mode"])
	}
	columns, _ := row["columns"].([]any)
	if len(columns) != 2 {
		t.Fatalf("decision row columns = %#v", columns)
	}
	for index, raw := range columns {
		column, _ := raw.(map[string]any)
		buttons, _ := column["elements"].([]any)
		if len(buttons) != 1 {
			t.Fatalf("column %d elements = %#v", index, buttons)
		}
		button, _ := buttons[0].(map[string]any)
		if button["tag"] != "button" || button["size"] != "small" || button["width"] != "fill" {
			t.Fatalf("column %d button = %#v", index, button)
		}
		if _, isSubmit := button["action_type"]; isSubmit {
			t.Fatalf("compact callback button must not submit a form: %#v", button)
		}
		behaviors, _ := button["behaviors"].([]any)
		if len(behaviors) != 1 {
			t.Fatalf("column %d behaviors = %#v", index, behaviors)
		}
		behavior, _ := behaviors[0].(map[string]any)
		value, _ := behavior["value"].(map[string]any)
		if behavior["type"] != "callback" || value["action"] != callbackAction ||
			value["task_id"] != float64(7) || value["version"] != float64(12) {
			t.Fatalf("column %d callback = %#v", index, behavior)
		}
	}
}

func TestSendQuestionFoldsProgressAndUsesTextDetailLink(t *testing.T) {
	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), testNotice()); err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}

	elements := cardElements(t, sentCard(t, lark))
	panel := onlyElementWithTag(t, elements, "collapsible_panel")
	if expanded, ok := panel["expanded"].(bool); !ok || expanded {
		t.Fatalf("progress panel must start folded: %#v", panel)
	}
	header, _ := panel["header"].(map[string]any)
	title, _ := header["title"].(map[string]any)
	if title["content"] != "目前进展" {
		t.Fatalf("progress panel header = %#v", header)
	}
	panelElements, _ := panel["elements"].([]any)
	if len(panelElements) != 1 || panelElements[0].(map[string]any)["content"] != "已核对三份数据" {
		t.Fatalf("progress panel elements = %#v", panelElements)
	}

	detail, _ := elements[len(elements)-1].(map[string]any)
	if detail["tag"] != "markdown" || detail["text_size"] != "notation" ||
		!strings.Contains(detail["content"].(string), "[查看详情](http://192.168.1.20:18800/#/work/task/7)") {
		t.Fatalf("detail link = %#v", detail)
	}
	if strings.Contains(mustJSON(t, detail), `"tag":"button"`) {
		t.Fatalf("detail link must not be a button: %#v", detail)
	}
}

func TestSendQuestionKeepsCompleteApprovalBody(t *testing.T) {
	notice := testNotice()
	notice.Question.Body = "背景：需要确认这份长文档。\n\n" + strings.Repeat("完整正文", 600)
	notice.Question.Fields = []execute.QuestionField{
		{Type: execute.FieldButton, Name: "confirm", Label: "确认", Style: "primary"},
		{Type: execute.FieldButton, Name: "reject", Label: "拒绝", Style: "danger"},
	}
	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), notice); err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}

	elements := cardElements(t, sentCard(t, lark))
	body, _ := elements[0].(map[string]any)
	if body["content"] != notice.Question.Body {
		t.Fatalf("approval body was truncated: got %d runes, want %d", len([]rune(body["content"].(string))), len([]rune(notice.Question.Body)))
	}
}

// TestSendQuestionOmitsBlankNames pins a Feishu boundary that already broke a
// real card: a form child may carry no name at all, but never an empty one
// ("name can not be empty string", 200530). Links are the only unnamed field,
// because they answer nothing.
func TestSendQuestionOmitsBlankNames(t *testing.T) {
	notice := testNotice()
	notice.Question.Fields = append(notice.Question.Fields,
		execute.QuestionField{Type: execute.FieldLink, Label: "打开后台", URL: "https://example.com/admin"})

	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), notice); err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}
	elements, _ := onlyForm(t, sentCard(t, lark))["elements"].([]any)
	unnamed := 0
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		if element["tag"] == "markdown" {
			continue // a select/multi_select label, not a control
		}
		name, present := element["name"]
		if !present {
			unnamed++
			if element["action_type"] != "link" {
				t.Fatalf("only links may be unnamed: %#v", element)
			}
			continue
		}
		if strings.TrimSpace(name.(string)) == "" {
			t.Fatalf("blank name would be rejected by Feishu: %#v", element)
		}
	}
	if unnamed != 1 {
		t.Fatalf("want exactly the unnamed link, got %d unnamed elements", unnamed)
	}
}

// TestSendQuestionBindsIdempotencyToTaskVersion keeps a retried send from
// producing a second card the principal could answer twice.
func TestSendQuestionBindsIdempotencyToTaskVersion(t *testing.T) {
	lark := &fakeLark{response: map[string]any{"data": map[string]any{"message_id": "om_card"}}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), testNotice()); err != nil {
		t.Fatalf("SendQuestion() error = %v", err)
	}
	if !strings.Contains(strings.Join(lark.args, " "), "jarvis-ask-7-v12") {
		t.Fatalf("args = %v", lark.args)
	}
}

// TestAnsweredCardDropsControls: once answered, the card must state the outcome
// and stop offering anything to click, since the Task has already moved on.
func TestAnsweredCardDropsControls(t *testing.T) {
	card, err := testNotifier(t, &fakeLark{}).AnsweredCard(testNotice(), "已收到你的回答，正在继续。")
	if err != nil {
		t.Fatalf("AnsweredCard() error = %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(card, &decoded); err != nil {
		t.Fatalf("decode answered card: %v", err)
	}
	if strings.Contains(string(card), "form_submit") {
		t.Fatalf("answered card still offers a submit button: %s", card)
	}
	if !strings.Contains(string(card), "已收到你的回答") {
		t.Fatalf("answered card lost the outcome: %s", card)
	}
	if !strings.Contains(string(card), "拟发送：本轮评测通过率 92%") {
		t.Fatalf("answered card must keep the question as audit text: %s", card)
	}
}

func TestSendQuestionRejectsAmbiguousMessageID(t *testing.T) {
	lark := &fakeLark{response: map[string]any{
		"data": []any{map[string]any{"message_id": "om_a"}, map[string]any{"message_id": "om_b"}},
	}}
	if _, err := testNotifier(t, lark).SendQuestion(context.Background(), testNotice()); err == nil {
		t.Fatal("two message_ids must fail rather than pick one")
	}
}

func sentCard(t *testing.T, lark *fakeLark) map[string]any {
	t.Helper()
	for index, arg := range lark.args {
		if arg == "--content" && index+1 < len(lark.args) {
			var card map[string]any
			if err := json.Unmarshal([]byte(lark.args[index+1]), &card); err != nil {
				t.Fatalf("decode sent card: %v", err)
			}
			return card
		}
	}
	t.Fatalf("no --content in args %v", lark.args)
	return nil
}

func onlyForm(t *testing.T, card map[string]any) map[string]any {
	t.Helper()
	elements := cardElements(t, card)
	var forms []map[string]any
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		if element["tag"] == "form" {
			forms = append(forms, element)
		}
	}
	if len(forms) != 1 {
		t.Fatalf("card must hold exactly one form, got %d: %#v", len(forms), elements)
	}
	return forms[0]
}

func cardElements(t *testing.T, card map[string]any) []any {
	t.Helper()
	body, ok := card["body"].(map[string]any)
	if !ok {
		t.Fatalf("card body = %#v", card["body"])
	}
	elements, ok := body["elements"].([]any)
	if !ok {
		t.Fatalf("card elements = %#v", body["elements"])
	}
	return elements
}

func onlyElementWithTag(t *testing.T, elements []any, tag string) map[string]any {
	t.Helper()
	var matches []map[string]any
	for _, raw := range elements {
		element, _ := raw.(map[string]any)
		if element["tag"] == tag {
			matches = append(matches, element)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one %s, got %d: %#v", tag, len(matches), elements)
	}
	return matches[0]
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal value: %v", err)
	}
	return string(encoded)
}
