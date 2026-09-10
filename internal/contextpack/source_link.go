package contextpack

import (
	"encoding/json"
	"net/url"
	"slices"
)

// SourceURL projects the single trigger's captured link for task and card
// presentation. Old captures without a trigger/link have no jump target; never
// guess from citation order, model notes, message text or the surrounding chat.
func SourceURL(raw []byte) string {
	var packet struct {
		Source  json.RawMessage `json:"source"`
		Capture struct {
			Messages []struct {
				ID  string `json:"message_id"`
				URL string `json:"source_url"`
			} `json:"messages"`
		} `json:"capture"`
	}
	if json.Unmarshal(raw, &packet) != nil {
		return ""
	}
	var source struct {
		Trigger string   `json:"trigger_message_id"`
		IDs     []string `json:"source_message_ids"`
	}
	if json.Unmarshal(packet.Source, &source) != nil || source.Trigger == "" || !slices.Contains(source.IDs, source.Trigger) {
		return ""
	}
	for _, message := range packet.Capture.Messages {
		if message.ID != source.Trigger {
			continue
		}
		u, err := url.Parse(message.URL)
		if err == nil && u.Host != "" && (u.Scheme == "https" || u.Scheme == "http") {
			return message.URL
		}
		return ""
	}
	return ""
}
