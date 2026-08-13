package background

import (
	"testing"

	"jarvis/internal/domain"
)

func TestToProfileViewIncludesSubjectID(t *testing.T) {
	profile := domain.PrincipalProfile{ID: 17, OpenID: "ou_principal", Name: "Principal"}

	view := toProfileView(&profile)

	if view.ID != profile.ID || view.OpenID != profile.OpenID || view.Name != profile.Name {
		t.Fatalf("toProfileView() = %+v, want identity fields from profile", view)
	}
}
