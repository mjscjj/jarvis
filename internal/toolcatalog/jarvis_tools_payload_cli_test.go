package toolcatalog

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"
)

// Execute the literal examples from offline help through HTTP and the real
// domain validators, using only an isolated temporary database.
func TestJarvisToolsWorldHelpExamplesPassRealServices(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "cli.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	projects, err := background.NewProjectService(db)
	if err != nil {
		t.Fatal(err)
	}
	persons, err := background.NewPersonService(db)
	if err != nil {
		t.Fatal(err)
	}
	matters, err := background.NewKeyMatterService(db)
	if err != nil {
		t.Fatal(err)
	}
	resources, err := background.NewResourceService(db)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := background.NewGroupBackgroundService(db, nil)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := background.NewProfileService(db, "ou_principal")
	if err != nil {
		t.Fatal(err)
	}
	seedProject := domain.Project{ID: 7, Name: "Old project", Role: "owner", Status: "active", Priority: 1}
	seedPerson := domain.Person{ID: 7, OpenID: "ou_seed", Name: "Old person", Role: "key"}
	seedMatter := domain.KeyMatter{ID: 7, Title: "Old matter"}
	seedResource := domain.ManagedResource{ID: 7, Title: "Old resource", ResourceType: "note", IsActive: true}
	seedGroup := domain.Group{ID: 7, ChatID: "oc_seed"}
	for _, seed := range []any{&seedProject, &seedPerson, &seedMatter, &seedResource, &seedGroup} {
		if err := db.Create(seed).Error; err != nil {
			t.Fatal(err)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var result any
		var err error
		switch r.Method + " " + r.URL.Path {
		case "POST /api/projects", "PUT /api/projects/7":
			var input background.ProjectInput
			if err = decoder.Decode(&input); err == nil {
				if r.Method == "POST" {
					result, err = projects.Create(r.Context(), input)
				} else {
					result, err = projects.Update(r.Context(), 7, input)
				}
			}
		case "POST /api/persons":
			var input background.PersonCreateInput
			if err = decoder.Decode(&input); err == nil {
				result, err = persons.Create(r.Context(), input)
			}
		case "PUT /api/persons/7":
			var input background.PersonUpdateInput
			if err = decoder.Decode(&input); err == nil {
				result, err = persons.Update(r.Context(), 7, input)
			}
		case "POST /api/key-matters", "PUT /api/key-matters/7":
			var input background.KeyMatterInput
			if err = decoder.Decode(&input); err == nil {
				if r.Method == "POST" {
					result, err = matters.Create(r.Context(), input)
				} else {
					result, err = matters.Update(r.Context(), 7, input)
				}
			}
		case "POST /api/resources", "PUT /api/resources/7":
			var input background.ResourceInput
			if err = decoder.Decode(&input); err == nil {
				if r.Method == "POST" {
					result, err = resources.Create(r.Context(), input)
				} else {
					result, err = resources.Update(r.Context(), 7, input)
				}
			}
		case "PUT /api/profile":
			var input background.ProfileInput
			if err = decoder.Decode(&input); err == nil {
				result, err = profile.Upsert(r.Context(), input)
			}
		case "PUT /api/groups/7":
			var input background.GroupBackgroundInput
			if err = decoder.Decode(&input); err == nil {
				result, err = groups.UpdateBackground(r.Context(), 7, input)
			}
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if err != nil {
			t.Errorf("%s %s help example failed validation: %v", r.Method, r.URL.Path, err)
			http.Error(w, err.Error(), 400)
			return
		}
		if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": result}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	for _, command := range []string{"create-project", "update-project", "create-person", "update-person", "create-key-matter", "update-key-matter", "create-resource", "update-resource", "update-principal", "update-group"} {
		t.Run(command, func(t *testing.T) {
			help, err := runJarvisTools(t, "", nil, command, "--help")
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []string{"not PATCH", "summary", "update-page", "Read back:", "Returns"} {
				if !strings.Contains(help, required) {
					t.Errorf("%s help missing %q", command, required)
				}
			}
			if strings.Contains(help, "observer") {
				t.Errorf("help invented project role: %s", help)
			}
			match := regexp.MustCompile(`(?m)^  jarvis-tools ` + regexp.QuoteMeta(command) + `( --id 7)? --payload '([^']+)'$`).FindStringSubmatch(help)
			if match == nil {
				t.Fatalf("minimal example missing: %s", help)
			}
			args := []string{command}
			if match[1] != "" {
				args = append(args, "--id", "7")
			}
			args = append(args, "--payload", match[2])
			out, err := runJarvisTools(t, server.URL, nil, args...)
			if err != nil || !json.Valid([]byte(out)) || !strings.Contains(out, `"id":`) {
				t.Fatalf("example %v output=%s err=%v", args, out, err)
			}
		})
	}
}

func TestJarvisToolsDatesRetainAutumnDSTAndNaturalDayFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if from, until := r.URL.Query().Get("from"), r.URL.Query().Get("until"); from != "2026-11-01T00:00:00-04:00" || until != "2026-11-02T00:00:00-05:00" {
			t.Errorf("wrong 25-hour day: %s", r.URL)
		}
		fmt.Fprint(w, `{"code":0,"data":{"total":0,"page":1,"page_size":20,"items":[]}}`)
	}))
	defer server.Close()
	for _, command := range []string{"list-tasks", "list-todos", "list-facts", "query-messages"} {
		args := []string{command, "--date", "2026-11-01"}
		if command == "list-facts" {
			args = append(args, "--subject-type", "project", "--subject-id", "7")
		}
		if _, err := runJarvisTools(t, server.URL, []string{"JARVIS_TIMEZONE=America/New_York"}, args...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestJarvisToolsListProjectionsPreserveLooseNestedJSON(t *testing.T) {
	const evidence = `{"number":9007199254740993,"decimal":0.123456789012345678901,"unknown":[1e400]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"total":1,"page":1,"page_size":20,"items":[{"id":7,"target":`+evidence+`,"resolution":`+evidence+`,"source_quote":`+evidence+`,"unknown":`+evidence+`}]}}`)
	}))
	defer server.Close()
	for _, command := range []string{"list-tasks", "list-todos", "query-resources"} {
		out, err := runJarvisTools(t, server.URL, nil, command)
		if err != nil || !strings.Contains(out, evidence) {
			t.Fatalf("%s changed nested evidence: %s err=%v", command, out, err)
		}
	}
}
