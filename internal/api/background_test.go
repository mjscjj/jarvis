package api

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"jarvis/internal/background"
	"jarvis/internal/config"
	"jarvis/internal/domain"
	"jarvis/internal/store"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func TestBackgroundListQueriesSQLite(t *testing.T) {
	db, err := store.OpenSQLite(t.Context(), config.SQLiteConfig{Path: filepath.Join(t.TempDir(), "jarvis.db")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}
	text := func(value string) *string { return &value }
	id := func(value uint64) *uint64 { return &value }
	now := time.Now().UTC()
	seed := func(value any) {
		t.Helper()
		if err := db.Create(value).Error; err != nil {
			t.Fatal(err)
		}
	}
	seed(&[]domain.Project{
		{ID: 1, Code: text(`P_%\`), Name: "ProjectName", Role: "owner", Status: "active", Priority: 1, Summary: text(`needle %_\`)},
		{ID: 2, Code: text(`P_%\-suffix`), Name: "Second", Role: "owner", Status: "active", Priority: 1, Summary: text("needle")},
		{ID: 3, Name: "Third", Role: "owner", Status: "paused", Priority: 1, Summary: text("needle")},
		{ID: 4, Name: "Other", Role: "participant", Status: "planning", Priority: 1},
		{ID: 5, Name: "Archived", Role: "owner", Status: "archived", Priority: 1, Summary: text(`needle %_\`)},
	})
	seed(&[]domain.Person{
		{ID: 1, OpenID: `ou_%\`, Name: "PersonName", Department: text(`needle %_\`), Role: "leader", PriorityWeight: 0.5},
		{ID: 2, OpenID: `ou_%\-suffix`, Name: "SecondPerson", Department: text("needle"), Role: "leader", PriorityWeight: 0.5},
		{ID: 3, OpenID: "ou_third", Name: "ThirdPerson", Department: text("needle"), Role: "leader", PriorityWeight: 0.5},
		{ID: 4, OpenID: "ou_other", Name: "OtherPerson", Department: text("needle"), Role: "colleague", PriorityWeight: 0.5},
	})
	seed(&[]domain.KeyMatter{
		{ID: 1, Title: "FirstMatter", Status: "推进中", Summary: text(`needle %_\`), LastActiveAt: now},
		{ID: 2, Title: "SecondMatter", Summary: text("needle"), LastActiveAt: now},
		{ID: 3, Title: "ThirdMatter", Summary: text("needle"), LastActiveAt: now},
		{ID: 4, Title: "ClosedMatter", Summary: text("needle"), ClosedAt: &now, LastActiveAt: now},
		{ID: 5, Title: "OtherMatter", LastActiveAt: now},
	})
	seed(&[]domain.ManagedResource{
		{ID: 1, Title: "FirstResource", ResourceType: "repo", Summary: text(`needle %_\`), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 2, Title: "SecondResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 3, Title: "ThirdResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
		{ID: 4, Title: "OtherResource", ResourceType: "doc", PersonID: id(2), LastActiveAt: now},
		{ID: 5, Title: "InactiveResource", ResourceType: "doc", Summary: text("needle"), PersonID: id(1), ProjectID: id(1), LinkPrincipal: true, LastActiveAt: now},
	})
	if err := db.Model(&domain.ManagedResource{}).Where("id = ?", 5).UpdateColumn("is_active", false).Error; err != nil {
		t.Fatal(err)
	}
	seed(&[]domain.Group{
		{ID: 1, ChatID: `oc_%\`, ChatMode: "group", Name: text("needle"), Tier: "hot"},
		{ID: 2, ChatID: `oc_%\-suffix`, ChatMode: "group", Name: text("needle"), Tier: "cold", RelatedGroup: true},
	})

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
	h := server.New()
	h.GET("/api/projects", ListProjects(projects))
	h.GET("/api/persons", ListPersons(persons))
	h.GET("/api/key-matters", ListKeyMatters(matters))
	h.GET("/api/resources", ListResources(resources))
	h.GET("/api/groups", ListGroups(groups))

	cases := []struct {
		name, path, query string
		ids               []uint64
		total             int64
	}{
		{"project summary second page", "projects", "keyword=needle&page=2&page_size=2", []uint64{1}, 3},
		{"project role keyword", "projects", "keyword=participant", []uint64{4}, 1},
		{"project status keyword", "projects", "keyword=paused", []uint64{3}, 1},
		{"project exact code", "projects", "code=" + url.QueryEscape(`P_%\`), []uint64{1}, 1},
		{"project partial code", "projects", "code=P_", []uint64{}, 0},
		{"project whitespace remains exact", "projects", "code=" + url.QueryEscape(` P_%\ `), []uint64{}, 0},
		{"project code and keyword intersect", "projects", "code=" + url.QueryEscape(`P_%\`) + "&keyword=paused", []uint64{}, 0},
		{"project special keyword", "projects", "keyword=" + url.QueryEscape(`needle %_\`), []uint64{1}, 1},
		{"person second page", "persons", "keyword=needle&role=leader&page=2&page_size=2", []uint64{1}, 3},
		{"person exact open id", "persons", "open_id=" + url.QueryEscape(`ou_%\`), []uint64{1}, 1},
		{"person partial open id", "persons", "open_id=ou_", []uint64{}, 0},
		{"person open id remains case sensitive", "persons", "open_id=" + url.QueryEscape(`OU_%\`), []uint64{}, 0},
		{"person role and open id intersect", "persons", "open_id=" + url.QueryEscape(`ou_%\`) + "&role=colleague", []uint64{}, 0},
		{"person exact role", "persons", "role=lead", []uint64{}, 0},
		{"person special keyword", "persons", "keyword=" + url.QueryEscape(`needle %_\`), []uint64{1}, 1},
		{"matter summary second page", "key-matters", "keyword=needle&page=2&page_size=2", []uint64{1}, 3},
		{"matter include closed", "key-matters", "keyword=needle&include_closed=true&page=2&page_size=2", []uint64{2, 1}, 4},
		{"matter special keyword", "key-matters", "keyword=" + url.QueryEscape(`%_\`), []uint64{1}, 1},
		{"matter empty", "key-matters", "keyword=missing", []uint64{}, 0},
		{"resource all filters second page", "resources", "keyword=needle&person_id=1&project_id=1&person_open_id=" + url.QueryEscape(`ou_%\`) + "&principal_only=true&active_only=true&page=2&page_size=2", []uint64{1}, 3},
		{"resource exact person open id", "resources", "person_open_id=" + url.QueryEscape(`ou_%\`), []uint64{5, 3, 2, 1}, 4},
		{"resource person name", "resources", "keyword=PersonName", []uint64{5, 3, 2, 1}, 4},
		{"resource project name", "resources", "keyword=ProjectName", []uint64{5, 3, 2, 1}, 4},
		{"resource special keyword", "resources", "keyword=" + url.QueryEscape(`%_\`), []uint64{1}, 1},
		{"resource missing person returns empty", "resources", "person_open_id=ou_missing", []uint64{}, 0},
		{"resource missing person id returns empty", "resources", "person_id=999", []uint64{}, 0},
		{"resource partial person open id", "resources", "person_open_id=ou_", []uint64{}, 0},
		{"group exact chat id", "groups", "chat_id=" + url.QueryEscape(`oc_%\`), []uint64{1}, 1},
		{"group partial chat id", "groups", "chat_id=oc_", []uint64{}, 0},
		{"group related filter retained", "groups", "chat_id=" + url.QueryEscape(`oc_%\`) + "&related_only=true", []uint64{}, 0},
		{"group keyword still broadens", "groups", "chat_id=" + url.QueryEscape(`oc_%\`) + "&related_only=true&keyword=needle", []uint64{1}, 1},
		{"group keyword and id intersect", "groups", "chat_id=" + url.QueryEscape(`oc_%\`) + "&keyword=missing", []uint64{}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := ut.PerformRequest(h.Engine, "GET", "/api/"+tc.path+"?"+tc.query, nil).Result()
			if response.StatusCode() != consts.StatusOK {
				t.Fatalf("status=%d body=%s", response.StatusCode(), response.Body())
			}
			var payload struct {
				Code int `json:"code"`
				Data struct {
					Items []struct {
						ID uint64 `json:"id"`
					} `json:"items"`
					Total       int64 `json:"total"`
					Page        int   `json:"page"`
					PageSize    int   `json:"page_size"`
					ActiveTotal int64 `json:"active_total"`
				} `json:"data"`
			}
			if err := json.Unmarshal(response.Body(), &payload); err != nil {
				t.Fatal(err)
			}
			ids := make([]uint64, 0, len(payload.Data.Items))
			for _, item := range payload.Data.Items {
				ids = append(ids, item.ID)
			}
			if payload.Code != 0 || payload.Data.Items == nil || payload.Data.Total != tc.total || !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("body=%s, want total=%d ids=%v and non-null items", response.Body(), tc.total, tc.ids)
			}
			query, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatal(err)
			}
			page, size := 1, 20
			if query.Get("page") == "2" {
				page, size = 2, 2
			}
			if payload.Data.Page != page || payload.Data.PageSize != size {
				t.Fatalf("pagination changed: body=%s", response.Body())
			}
			if tc.path == "resources" && payload.Data.ActiveTotal != 4 {
				t.Fatalf("global active_total changed: body=%s", response.Body())
			}
		})
	}
}
