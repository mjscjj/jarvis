package chat

import "testing"

func TestParseTRAEModelsPreservesSourceOrderAndCapabilities(t *testing.T) {
	t.Parallel()
	models, err := parseTRAEModels([]byte(`[
  {"name":"Doubao-Seed-2.1-Pro","real_name":"Seed-2.1-Pro","description":"200K context window","supported_mime_types":["image/*"]},
  {"name":"gpt-5.6-sol","description":"reasoning","supported_mime_types":[]}
]`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "Doubao-Seed-2.1-Pro" || models[0].Name != "Seed-2.1-Pro" || !models[0].Default {
		t.Fatalf("models = %#v", models)
	}
	if len(models[0].InputModalities) != 2 || models[0].InputModalities[1] != "image" {
		t.Fatalf("image capabilities = %#v", models[0].InputModalities)
	}
	if models[1].Name != "gpt-5.6-sol" || models[1].Default {
		t.Fatalf("fallback model = %#v", models[1])
	}
}

func TestParseCursorModelsIgnoresHeadingsAndFindsAuto(t *testing.T) {
	t.Parallel()
	models, err := parseCursorModels("Available models\n\nauto - Auto (default)\ngpt-5.6-sol-high - GPT-5.6 Sol 1M High\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "auto" || !models[0].Default || models[1].ID != "gpt-5.6-sol-high" {
		t.Fatalf("models = %#v", models)
	}
}

func TestParseCursorModelsFailsOnProtocolDrift(t *testing.T) {
	t.Parallel()
	if _, err := parseCursorModels("Available models\n(no model lines)\n"); err == nil {
		t.Fatal("parseCursorModels() must fail when the upstream format has no model rows")
	}
}
