package api

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"jarvis/internal/larkcli"
)

type stubOKRAvatarDirectory struct {
	people map[string]larkcli.DirectoryPerson
	errs   map[string]error
}

func (directory *stubOKRAvatarDirectory) Avatar(_ context.Context, email string) (larkcli.DirectoryPerson, error) {
	if err := directory.errs[email]; err != nil {
		return larkcli.DirectoryPerson{}, err
	}
	return directory.people[email], nil
}

func TestGetOKRDirectoryAvatars(t *testing.T) {
	directory := &stubOKRAvatarDirectory{people: map[string]larkcli.DirectoryPerson{
		"a@example.test": {Email: "a@example.test", Name: "甲", AvatarURL: "https://example.test/a.png"},
	}}
	h := server.New()
	h.GET("/avatars", getOKRDirectoryAvatars(directory))
	response := ut.PerformRequest(h.Engine, "GET", "/avatars?emails=a@example.test,a@example.test", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data struct {
			People []larkcli.DirectoryPerson `json:"people"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.People) != 1 || payload.Data.People[0].AvatarURL == "" {
		t.Fatalf("people=%+v", payload.Data.People)
	}
}

func TestGetOKRDirectoryAvatarsFailsFast(t *testing.T) {
	for _, test := range []struct {
		name      string
		query     string
		directory okrAvatarDirectory
		status    int
	}{
		{name: "blank", query: "", directory: &stubOKRAvatarDirectory{}, status: 400},
		{name: "too many", query: "1@x,2@x,3@x,4@x,5@x,6@x,7@x,8@x,9@x", directory: &stubOKRAvatarDirectory{}, status: 400},
		{name: "all failed", query: "b@example.test", directory: &stubOKRAvatarDirectory{errs: map[string]error{"b@example.test": fmt.Errorf("token expired")}}, status: 502},
	} {
		t.Run(test.name, func(t *testing.T) {
			h := server.New()
			h.GET("/avatars", getOKRDirectoryAvatars(test.directory))
			response := ut.PerformRequest(h.Engine, "GET", "/avatars?emails="+test.query, nil).Result()
			if response.StatusCode() != test.status {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
		})
	}
}

func TestGetOKRDirectoryAvatarsReportsPartialFailure(t *testing.T) {
	directory := &stubOKRAvatarDirectory{
		people: map[string]larkcli.DirectoryPerson{"a@example.test": {Email: "a@example.test", AvatarURL: "https://example.test/a.png"}},
		errs:   map[string]error{"b@example.test": fmt.Errorf("token expired")},
	}
	h := server.New()
	h.GET("/avatars", getOKRDirectoryAvatars(directory))
	response := ut.PerformRequest(h.Engine, "GET", "/avatars?emails=a@example.test,b@example.test", nil).Result()
	if response.StatusCode() != 200 {
		t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
	}
	var payload struct {
		Data struct {
			People       []larkcli.DirectoryPerson `json:"people"`
			FailedEmails []string                  `json:"failed_emails"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.People) != 1 || len(payload.Data.FailedEmails) != 1 || payload.Data.FailedEmails[0] != "b@example.test" {
		t.Fatalf("data=%+v", payload.Data)
	}
}
