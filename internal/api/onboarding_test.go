package api

import "testing"

func TestOnboardingFinalizeDoesNotAcceptAnotherAppID(t *testing.T) {
	var request finalizeOnboardingRequest
	if err := decodeStrictJSON([]byte(`{"app_id":"cli_another","app_secret":"secret"}`), &request); err == nil {
		t.Fatal("frontend must not choose an App ID independently of lark-cli")
	}
	if err := decodeStrictJSON([]byte(`{"app_secret":"secret"}`), &request); err != nil {
		t.Fatal(err)
	}
	if request.AppSecret != "secret" || request.AgentName != "" {
		t.Fatal("unexpected onboarding input")
	}
}
