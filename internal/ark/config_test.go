package ark

import "testing"

func TestBaseURLUsesCodingPath(t *testing.T) {
	const want = "https://ark.cn-beijing.volces.com/api/coding/v3"
	if BaseURL != want {
		t.Fatalf("BaseURL = %q, want %q", BaseURL, want)
	}
}
