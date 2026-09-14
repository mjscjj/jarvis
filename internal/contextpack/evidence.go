package contextpack

import (
	"encoding/json"
	"fmt"
	"sort"
	"unicode/utf8"
)

const EvidenceBudget = 30000

// FreezeEvidence writes the current carrier. Source is producer-owned original
// input; admission reasoning belongs in the Todo event, never in this packet.
func FreezeEvidence(source, capture, annotation []byte) (json.RawMessage, error) {
	if len(annotation) == 0 {
		annotation = []byte(`{}`)
	}
	raw := encoded(Object{"format_version": encoded(2), "source": source, "capture": capture, "annotation": annotation})
	if _, err := SourceMessages(raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// Evidence uses origin only to interpret the historical unversioned protocol.
// Current packets have identical semantics for every producer.
func Evidence(raw []byte, origin string) (json.RawMessage, error) {
	if err := Validate(raw); err != nil {
		return nil, err
	}
	packet, _ := object(raw)
	capture, _ := object(packet["capture"])
	result := Object{"read": encoded("get-task/get-todo --id ID --context SECTION; --message-id MESSAGE_ID; --offset N --length N (Unicode characters)"), "source_role": encoded("original request or event references; index labels are not verified intent")}
	source := packet["source"]
	if _, versioned := packet["format_version"]; !versioned && origin == "todo" {
		old, _ := object(source)
		refs := Object{}
		for _, key := range []string{"source_message_ids", "trigger_message_id", "source_quote"} {
			if v, ok := old[key]; ok {
				refs[key] = v
			}
		}
		source = encoded(refs)
		result["legacy_admission_read"] = encoded("--context source: historical M3 judgment, not original instructions")
	}
	result["source"] = source
	// Only scene fields enter the reading view. Long historical world pages stay
	// available verbatim behind section reads, not mixed with current world state.
	for _, key := range []string{"captured_at", "group", "participants", "resources", "request_context", "coverage", "evidence_refs", "project_association"} {
		if body, ok := capture[key]; ok {
			result[key] = body
		}
	}
	for _, key := range []string{"group", "participants", "resources"} {
		if body, ok := result[key]; ok {
			result[key] = sceneMetadata(body)
		}
	}
	notes, _ := object(packet["annotation"])
	if id, ok := notes["delegation_id"]; ok {
		result["associations"] = encoded(Object{"delegation_id": id})
	}
	// Large source or scene fields stay losslessly available through exact
	// section/range reads. Project them before assigning the message budget so a
	// large manual request cannot accidentally crowd out every conversation row.
	for key, body := range result {
		if utf8.RuneCount(body) > EvidenceBudget/2 {
			result[key] = encoded(map[string]any{"expanded": false, "characters": utf8.RuneCount(body), "read_section": key})
		}
	}
	for utf8.RuneCount(encoded(result)) > EvidenceBudget/2 {
		key, size := "", 0
		for candidate, body := range result {
			if candidate == "source" || candidate == "read" || candidate == "source_role" {
				continue
			}
			if n := utf8.RuneCount(body); n > size {
				key, size = candidate, n
			}
		}
		if key == "" || size < 200 {
			break
		}
		result[key] = encoded(map[string]any{"expanded": false, "characters": size, "read_section": key})
	}
	rows, _ := messages(capture)
	ids, _ := sourceIDs(source)
	priority := map[string]bool{}
	for _, id := range ids {
		priority[id] = true
	}
	byID := map[string]json.RawMessage{}
	for _, row := range rows {
		byID[messageID(row)] = row
	}
	queue := append([]string(nil), ids...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		row, _ := object(byID[id])
		for _, k := range []string{"reply_to", "root_id"} {
			var parent string
			_ = json.Unmarshal(row[k], &parent)
			if parent != "" && !priority[parent] {
				priority[parent] = true
				queue = append(queue, parent)
			}
		}
	}
	// Add primary evidence first, then nearest context; emit original order.
	selected := map[string]json.RawMessage{}
	// Keep room for coverage and continuation metadata added below.
	budget := EvidenceBudget - utf8.RuneCount(encoded(result)) - 2000
	add := func(row json.RawMessage) {
		n := utf8.RuneCount(row)
		if n <= budget {
			selected[messageID(row)] = row
			budget -= n
		}
	}
	for _, row := range rows {
		if priority[messageID(row)] {
			before := len(selected)
			add(row)
			if len(selected) == before {
				stub := messageMetadata(row)
				if utf8.RuneCount(stub) <= budget {
					selected[messageID(row)] = stub
					budget -= utf8.RuneCount(stub)
				}
			}
		}
	}
	for i := len(rows) - 1; i >= 0; i-- {
		if !priority[messageID(rows[i])] {
			add(rows[i])
		}
	}
	shown := []json.RawMessage{}
	omitted := []string{}
	for _, row := range rows {
		if body, ok := selected[messageID(row)]; ok {
			shown = append(shown, body)
		} else {
			omitted = append(omitted, messageID(row))
		}
	}
	result["messages"] = encoded(shown)
	result["conversation_message_count"] = encoded(len(rows))
	result["omitted_message_ids"] = encoded(omitted)
	available := []availableContext{
		{Name: "source", Bytes: len(packet["source"])},
		{Name: "annotation", Bytes: len(packet["annotation"])},
		{Name: "conversation", Bytes: len(capture["messages"])},
		{Name: "capture", Bytes: len(packet["capture"])},
		{Name: "full", Bytes: len(raw)},
	}
	keys := make([]string, 0, len(capture))
	for key, body := range capture {
		if key != "messages" && string(body) != "null" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		available = append(available, availableContext{Name: key, Bytes: len(capture[key])})
	}
	result["available_context"] = encoded(available)
	for utf8.RuneCount(encoded(result)) > EvidenceBudget && len(shown) > 0 {
		index := -1
		for i, row := range shown {
			if !priority[messageID(row)] {
				index = i
				break
			}
		}
		if index < 0 {
			for i, row := range shown {
				stub := messageMetadata(row)
				if !stringField(row, "body_omitted") && utf8.RuneCount(stub) < utf8.RuneCount(row) {
					shown[i] = stub
					result["messages"] = encoded(shown)
					index = -2
					break
				}
			}
		}
		if index == -2 {
			continue
		}
		if index < 0 {
			break
		}
		omitted = append(omitted, messageID(shown[index]))
		shown = append(shown[:index], shown[index+1:]...)
		result["messages"] = encoded(shown)
		result["omitted_message_ids"] = encoded(omitted)
	}
	if utf8.RuneCount(encoded(result)) > EvidenceBudget {
		return nil, fmt.Errorf("evidence metadata exceeds reading budget")
	}
	return encoded(result), nil
}

func messageMetadata(raw json.RawMessage) json.RawMessage {
	row, err := object(raw)
	if err != nil {
		return encoded(map[string]any{"body_omitted": true, "characters": utf8.RuneCount(raw), "read": "get-task/get-todo --message-id MESSAGE_ID --offset N --length N"})
	}
	characters := 0
	if body, ok := row["content"]; ok {
		var content string
		if json.Unmarshal(body, &content) == nil {
			characters = utf8.RuneCountInString(content)
		} else {
			characters = utf8.RuneCount(body)
		}
		delete(row, "content")
	}
	row["body_omitted"] = encoded(true)
	row["content_characters"] = encoded(characters)
	row["read"] = encoded("get-task/get-todo --id RECORD_ID --message-id " + messageID(raw) + " --offset N --length N")
	return encoded(row)
}

func stringField(raw json.RawMessage, key string) bool {
	row, err := object(raw)
	if err != nil {
		return false
	}
	var value bool
	_ = json.Unmarshal(row[key], &value)
	return value
}

// sceneMetadata projects known historical scene records, retaining identity and
// source fields but not old entity page bodies. Full originals remain in capture.
func sceneMetadata(raw json.RawMessage) json.RawMessage {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) == nil && rows != nil {
		for i := range rows {
			rows[i] = sceneMetadata(rows[i])
		}
		return encoded(rows)
	}
	obj, err := object(raw)
	if err != nil {
		return raw
	}
	delete(obj, "summary")
	delete(obj, "extracted_text")
	return encoded(obj)
}

// ReadFor is the entry point for model/API reads; raw section names remain exact.
func ReadFor(raw []byte, origin, section, id string) (json.RawMessage, error) {
	if id == "" && (section == "" || section == "overview" || section == "evidence") {
		return Evidence(raw, origin)
	}
	return Read(raw, section, id)
}

// Range returns a lossless slice of serialized JSON with its continuation.
func Range(raw []byte, offset, length int) (json.RawMessage, error) {
	if offset < 0 || length < 1 || length > 30000 {
		return nil, fmt.Errorf("offset must be nonnegative and length between 1 and 30000")
	}
	chars := []rune(string(raw))
	if offset > len(chars) {
		return nil, fmt.Errorf("offset exceeds content length")
	}
	end := min(len(chars), offset+length)
	return encoded(map[string]any{"text": string(chars[offset:end]), "offset": offset, "next_offset": end, "total_characters": len(chars), "has_more": end < len(chars)}), nil
}
