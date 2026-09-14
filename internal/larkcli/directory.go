package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DirectoryPerson exposes stable addresses, never app-local IDs, to products.
type DirectoryPerson struct {
	Email      string `json:"email"`
	Name       string `json:"name"`
	UnionID    string `json:"union_id,omitempty"`
	AvatarURL  string `json:"avatar_url,omitempty"`
	Department string `json:"department,omitempty"`
}

type Directory struct {
	avatarGate                          chan struct{}
	userCache                           directoryCache
	flights                             map[string]*directoryFlight
	client                              *Client
	profile, appID, identity, cacheFile string
	mu                                  sync.Mutex
	people                              []DirectoryPerson
	refreshed                           time.Time
}

// A user identity is an explicit, configured transition while the stable Bot
// awaits directory scopes. It is never inferred from the global CLI default.
func NewDirectory(ctx context.Context, client *Client, appID, profile, identity, cacheFile string) (*Directory, error) {
	if client == nil || appID == "" || profile == "" || (identity != "bot" && identity != "user") {
		return nil, fmt.Errorf("directory requires explicit app, profile and identity")
	}
	profiles, err := client.profiles(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := uniqueProfileForApp(profiles, appID, profile); err != nil {
		return nil, err
	}
	status, err := client.profileAuthStatus(ctx, profile)
	if err != nil {
		return nil, err
	}
	if status.AppID != appID || !status.Verified {
		return nil, fmt.Errorf("directory profile identity is not verified")
	}
	if identity == "user" && (!status.Identities.User.Verified || status.Identities.User.TokenStatus != "valid") {
		return nil, fmt.Errorf("configured directory user has no valid authorization")
	}
	if identity == "bot" && !status.Identities.Bot.Verified {
		return nil, fmt.Errorf("configured directory Bot is not verified")
	}
	d := &Directory{client: client, profile: profile, appID: appID, identity: identity, cacheFile: cacheFile}
	if identity == "user" {
		d.initUserCache(status.Identities.User.OpenID)
	}
	if identity == "bot" && cacheFile != "" {
		raw, err := os.ReadFile(cacheFile)
		if err == nil {
			var saved struct {
				AppID     string            `json:"app_id"`
				Refreshed time.Time         `json:"refreshed"`
				People    []DirectoryPerson `json:"people"`
			}
			if err := json.Unmarshal(raw, &saved); err != nil {
				return nil, fmt.Errorf("read directory cache: %w", err)
			}
			if saved.AppID == appID {
				d.people, d.refreshed = saved.People, saved.Refreshed
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return d, nil
}

func directoryEmail(value string) string {
	value = strings.TrimSpace(value)
	i := strings.LastIndexByte(value, '@')
	if i < 1 {
		return ""
	}
	value = value[:i] + "@" + strings.ToLower(value[i+1:])
	a, err := mail.ParseAddress(value)
	if err != nil || a.Address != value || a.Name != "" || !strings.Contains(value[i+1:], ".") {
		return ""
	}
	return value
}

func (d *Directory) Search(ctx context.Context, query string) ([]DirectoryPerson, error) {
	return d.search(ctx, query, true)
}
func (d *Directory) search(ctx context.Context, query string, avatars bool) ([]DirectoryPerson, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("请输入姓名或邮箱")
	}
	if d.identity == "user" {
		page, err := d.searchUserCached(ctx, query)
		return page.People, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	if d.refreshed.IsZero() || time.Since(d.refreshed) > 15*time.Minute {
		people, err := d.fetch(ctx)
		if err != nil {
			return nil, err
		}
		if err := d.save(people); err != nil {
			return nil, err
		}
		d.people, d.refreshed = people, time.Now().UTC()
	}
	result := []DirectoryPerson{}
	q := strings.ToLower(query)
	for _, p := range d.people {
		if strings.Contains(strings.ToLower(p.Name), q) || strings.Contains(strings.ToLower(p.Email), q) {
			result = append(result, p)
		}
	}
	if len(result) > 50 {
		return nil, fmt.Errorf("匹配人员较多，请输入更完整的姓名或邮箱")
	}
	return result, nil
}

func (d *Directory) Resolve(ctx context.Context, email string) (DirectoryPerson, error) {
	email = directoryEmail(email)
	if email == "" {
		return DirectoryPerson{}, fmt.Errorf("需要完整企业邮箱")
	}
	if d.identity == "user" {
		d.mu.Lock()
		p, ok := d.userCache.People[email]
		d.mu.Unlock()
		if ok && !p.Ambiguous && time.Since(p.At) < directoryTTL {
			return p.Person, nil
		}
	}
	people, err := d.search(ctx, email, false)
	if err != nil {
		return DirectoryPerson{}, err
	}
	var found []DirectoryPerson
	for _, p := range people {
		if p.Email == email {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		return DirectoryPerson{}, fmt.Errorf("企业邮箱 %s 未匹配到唯一有效人员", email)
	}
	return found[0], nil
}

type directoryUser struct {
	Name            string `json:"name"`
	Email           string `json:"email"`
	EnterpriseEmail string `json:"enterprise_email"`
	UnionID         string `json:"union_id"`
	Avatar          struct {
		URL string `json:"avatar_240"`
	} `json:"avatar"`
	Status struct {
		Resigned bool `json:"is_resigned"`
		Frozen   bool `json:"is_frozen"`
		Exited   bool `json:"is_exited"`
	} `json:"status"`
}

func (d *Directory) get(ctx context.Context, path string, params map[string]any, out any) error {
	raw, _ := json.Marshal(params)
	return d.client.Run(ctx, out, "--profile", d.profile, "api", "GET", path, "--params", string(raw), "--as", "bot")
}

func directoryNext(more bool, token string, seen map[string]bool) (bool, error) {
	if !more {
		return false, nil
	}
	if token == "" || seen[token] {
		return false, fmt.Errorf("directory returned invalid pagination")
	}
	seen[token] = true
	return true, nil
}

func (d *Directory) fetch(ctx context.Context) ([]DirectoryPerson, error) {
	ids, depts := map[string]bool{}, map[string]bool{}
	token := ""
	seen := map[string]bool{}
	for {
		var r struct {
			Data struct {
				Users       []string `json:"user_ids"`
				Departments []string `json:"department_ids"`
				Groups      []string `json:"group_ids"`
				HasMore     bool     `json:"has_more"`
				Token       string   `json:"page_token"`
			} `json:"data"`
		}
		if err := d.get(ctx, "/open-apis/contact/v3/scopes", map[string]any{"user_id_type": "union_id", "department_id_type": "open_department_id", "page_size": 100, "page_token": token}, &r); err != nil {
			return nil, err
		}
		if len(r.Data.Groups) > 0 {
			return nil, fmt.Errorf("directory group-only authorization needs explicit user/department scope before indexing")
		}
		for _, id := range r.Data.Users {
			ids[id] = true
		}
		for _, id := range r.Data.Departments {
			depts[id] = true
		}
		more, err := directoryNext(r.Data.HasMore, r.Data.Token, seen)
		if err != nil {
			return nil, err
		}
		if !more {
			break
		}
		token = r.Data.Token
	}
	// Expand only authorized roots, including each root's own users.
	roots := []string{}
	for id := range depts {
		roots = append(roots, id)
	}
	for _, root := range roots {
		token = ""
		seen = map[string]bool{}
		for {
			var r struct {
				Data struct {
					Items []struct {
						ID string `json:"open_department_id"`
					} `json:"items"`
					HasMore bool   `json:"has_more"`
					Token   string `json:"page_token"`
				} `json:"data"`
			}
			if err := d.get(ctx, "/open-apis/contact/v3/departments/"+url.PathEscape(root)+"/children", map[string]any{"department_id_type": "open_department_id", "fetch_child": true, "page_size": 50, "page_token": token}, &r); err != nil {
				return nil, err
			}
			for _, item := range r.Data.Items {
				depts[item.ID] = true
			}
			more, err := directoryNext(r.Data.HasMore, r.Data.Token, seen)
			if err != nil {
				return nil, err
			}
			if !more {
				break
			}
			token = r.Data.Token
		}
	}
	byEmail := map[string]DirectoryPerson{}
	add := func(users []directoryUser) error {
		for _, u := range users {
			if u.Status.Resigned || u.Status.Frozen || u.Status.Exited {
				continue
			}
			email := u.EnterpriseEmail
			if email == "" {
				email = u.Email
			}
			email = directoryEmail(email)
			if email == "" || u.Name == "" {
				continue
			}
			p := DirectoryPerson{Email: email, Name: u.Name, UnionID: u.UnionID, AvatarURL: u.Avatar.URL}
			if prev, ok := byEmail[email]; ok && prev.UnionID != "" && p.UnionID != "" && prev.UnionID != p.UnionID {
				return fmt.Errorf("directory has conflicting identities for %s", email)
			}
			byEmail[email] = p
		}
		return nil
	}
	for id := range depts {
		token = ""
		seen = map[string]bool{}
		for {
			var r struct {
				Data struct {
					Items   []directoryUser `json:"items"`
					HasMore bool            `json:"has_more"`
					Token   string          `json:"page_token"`
				} `json:"data"`
			}
			if err := d.get(ctx, "/open-apis/contact/v3/users/find_by_department", map[string]any{"department_id": id, "department_id_type": "open_department_id", "user_id_type": "union_id", "page_size": 50, "page_token": token}, &r); err != nil {
				return nil, err
			}
			if err := add(r.Data.Items); err != nil {
				return nil, err
			}
			more, err := directoryNext(r.Data.HasMore, r.Data.Token, seen)
			if err != nil {
				return nil, err
			}
			if !more {
				break
			}
			token = r.Data.Token
		}
	}
	users := []string{}
	for id := range ids {
		users = append(users, id)
	}
	for offset := 0; offset < len(users); offset += 50 {
		end := offset + 50
		if end > len(users) {
			end = len(users)
		}
		var r struct {
			Data struct {
				Items []directoryUser `json:"items"`
			} `json:"data"`
		}
		if err := d.get(ctx, "/open-apis/contact/v3/users/batch", map[string]any{"user_ids": users[offset:end], "user_id_type": "union_id"}, &r); err != nil {
			return nil, err
		}
		if err := add(r.Data.Items); err != nil {
			return nil, err
		}
	}
	result := []DirectoryPerson{}
	for _, p := range byEmail {
		result = append(result, p)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Email < result[j].Email })
	if len(result) == 0 {
		return nil, fmt.Errorf("通知应用目录未返回有效企业邮箱，请检查通讯录权限和数据范围")
	}
	return result, nil
}

func (d *Directory) save(people []DirectoryPerson) error {
	if d.cacheFile == "" {
		return nil
	}
	raw, err := json.Marshal(struct {
		AppID     string            `json:"app_id"`
		Refreshed time.Time         `json:"refreshed"`
		People    []DirectoryPerson `json:"people"`
	}{d.appID, time.Now().UTC(), people})
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(d.cacheFile), 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(d.cacheFile), "directory-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), d.cacheFile)
}

func (d *Directory) Avatar(ctx context.Context, email string) (DirectoryPerson, error) {
	email = directoryEmail(email)
	if email == "" {
		return DirectoryPerson{}, fmt.Errorf("需要完整企业邮箱")
	}
	if d.identity == "user" {
		return d.userAvatar(ctx, email)
	}
	people, err := d.search(ctx, email, true)
	if err != nil {
		return DirectoryPerson{}, err
	}
	var found []DirectoryPerson
	for _, p := range people {
		if p.Email == email {
			found = append(found, p)
		}
	}
	if len(found) != 1 {
		return DirectoryPerson{}, fmt.Errorf("邮箱未匹配唯一人员")
	}
	return found[0], nil
}
