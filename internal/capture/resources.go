package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"strings"
)

var (
	fileKeyPattern     = regexp.MustCompile(`(?i)(img_key|image_key|file_key):([a-z0-9_-]+)`)
	urlPattern         = regexp.MustCompile(`https?://[^\s\]\[()<>"']+`)
	docTokenPattern    = regexp.MustCompile(`/(?:docx|wiki)/([a-zA-Z0-9_-]+)`)
	minuteTokenPattern = regexp.MustCompile(`/(?:minutes|minute)/([a-zA-Z0-9_-]+)`)
)

type resourceRef struct {
	ResourceType string
	FileKey      string
	MinuteToken  *string
	DocToken     *string
	URL          *string
}

// extractResourceRefs only extracts identifiers explicitly present in the
// rendered lark-cli output. It never downloads or guesses resource content.
func extractResourceRefs(content string) []resourceRef {
	refs := make([]resourceRef, 0)
	seen := make(map[string]struct{})
	for _, match := range fileKeyPattern.FindAllStringSubmatch(content, -1) {
		key := match[2]
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		resourceType := "file"
		if strings.Contains(strings.ToLower(match[1]), "img") {
			resourceType = "image"
		}
		refs = append(refs, resourceRef{ResourceType: resourceType, FileKey: key})
	}

	for _, rawURL := range urlPattern.FindAllString(content, -1) {
		cleanURL := strings.TrimRight(rawURL, ".,;:!?")
		key := "link:" + sha256Hex(cleanURL)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ref := resourceRef{ResourceType: "link", FileKey: key, URL: &cleanURL}
		if match := minuteTokenPattern.FindStringSubmatch(cleanURL); len(match) == 2 {
			ref.ResourceType = "minutes"
			ref.MinuteToken = &match[1]
			ref.FileKey = "minutes:" + match[1]
		} else if match := docTokenPattern.FindStringSubmatch(cleanURL); len(match) == 2 {
			ref.ResourceType = "doc"
			ref.DocToken = &match[1]
			ref.FileKey = "doc:" + match[1]
		}
		refs = append(refs, ref)
	}
	return refs
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
