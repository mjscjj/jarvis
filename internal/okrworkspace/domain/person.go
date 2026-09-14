package domain

import (
	"net/mail"
	"strings"
)

// PersonRef is an OKR business identity. Provider-specific IDs never identify
// an owner or notification recipient. Name is a display snapshot.
type PersonRef struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	UnionID string `json:"union_id,omitempty"`
}

func NormalizeEmail(value string) string {
	value = strings.TrimSpace(value)
	i := strings.LastIndexByte(value, '@')
	if i < 0 {
		return value
	}
	return value[:i] + "@" + strings.ToLower(value[i+1:])
}

func ValidEmail(value string) bool {
	value = NormalizeEmail(value)
	address, err := mail.ParseAddress(value)
	if err != nil || address.Name != "" || address.Address != value {
		return false
	}
	i := strings.LastIndexByte(value, '@')
	return i > 0 && strings.Contains(value[i+1:], ".") && !strings.ContainsAny(value, "\r\n\t ")
}
