package larkcli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const directoryTTL = 5 * time.Minute

type directoryPage struct {
	People  []DirectoryPerson
	HasMore bool
	At      time.Time
}
type cachedDirectoryPerson struct {
	Ambiguous bool
	Person    DirectoryPerson
	OpenID    string
	At        time.Time
	AvatarAt  time.Time
}
type directoryFlight struct {
	done chan struct{}
	err  error
}
type directoryCache struct {
	AppID   string
	UserID  string
	Queries map[string]directoryPage
	People  map[string]cachedDirectoryPerson
}

func (d *Directory) initUserCache(userID string) {
	d.userCache = directoryCache{AppID: d.appID, UserID: userID, Queries: map[string]directoryPage{}, People: map[string]cachedDirectoryPerson{}}
	if d.cacheFile == "" || userID == "" {
		return
	}
	raw, err := os.ReadFile(d.cacheFile + ".user")
	if err != nil {
		return
	}
	var saved directoryCache
	if json.Unmarshal(raw, &saved) == nil && saved.AppID == d.appID && saved.UserID == userID && saved.Queries != nil && saved.People != nil {
		d.userCache = saved
	}
}

// Called under mu; the cache is bounded, private and scoped to both app and user.
func (d *Directory) saveUserCache() {
	for key, p := range d.userCache.Queries {
		if time.Since(p.At) > directoryTTL {
			delete(d.userCache.Queries, key)
		}
	}
	for key, p := range d.userCache.People {
		if time.Since(p.At) > directoryTTL {
			delete(d.userCache.People, key)
		}
	}
	if len(d.userCache.Queries) > 512 {
		for len(d.userCache.Queries) > 512 {
			var oldest string
			var at time.Time
			for k, v := range d.userCache.Queries {
				if oldest == "" || v.At.Before(at) {
					oldest, at = k, v.At
				}
			}
			delete(d.userCache.Queries, oldest)
		}
	}
	if len(d.userCache.People) > 2048 {
		for len(d.userCache.People) > 2048 {
			var oldest string
			var at time.Time
			for k, v := range d.userCache.People {
				if oldest == "" || v.At.Before(at) {
					oldest, at = k, v.At
				}
			}
			delete(d.userCache.People, oldest)
		}
	}
	if d.cacheFile == "" || d.userCache.UserID == "" {
		return
	}
	raw, err := json.Marshal(d.userCache)
	if err != nil {
		return
	}
	if os.MkdirAll(filepath.Dir(d.cacheFile), 0700) != nil {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(d.cacheFile), "people-cache-*")
	if err != nil {
		return
	}
	defer os.Remove(f.Name())
	_, err = f.Write(raw)
	closeErr := f.Close()
	if err == nil && closeErr == nil {
		_ = os.Rename(f.Name(), d.cacheFile+".user")
	}
}

// One canceled input must not cancel an identical query in another picker.
func (d *Directory) coalesce(ctx context.Context, key string, run func(context.Context) error) error {
	d.mu.Lock()
	if d.flights == nil {
		d.flights = map[string]*directoryFlight{}
	}
	f := d.flights[key]
	if f == nil {
		f = &directoryFlight{done: make(chan struct{})}
		d.flights[key] = f
		go func() {
			callCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			err := run(callCtx)
			d.mu.Lock()
			f.err = err
			delete(d.flights, key)
			close(f.done)
			d.mu.Unlock()
		}()
	}
	d.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-f.done:
		return f.err
	}
}

func (d *Directory) cachedPage(query string) (directoryPage, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	page, ok := d.userCache.Queries[query]
	if !ok || time.Since(page.At) > directoryTTL {
		return directoryPage{}, false
	}
	page.People = append([]DirectoryPerson{}, page.People...)
	for i, p := range page.People {
		if cached, ok := d.userCache.People[p.Email]; ok {
			page.People[i].AvatarURL = cached.Person.AvatarURL
		}
	}
	return page, true
}

func (d *Directory) searchUserCached(ctx context.Context, query string) (directoryPage, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return directoryPage{}, fmt.Errorf("请输入姓名或邮箱")
	}
	if p, ok := d.cachedPage(query); ok {
		return p, nil
	}
	err := d.coalesce(ctx, "search:"+query, func(ctx context.Context) error {
		if _, ok := d.cachedPage(query); ok {
			return nil
		}
		var response searchUserResponse
		if err := d.client.Run(ctx, &response, "--profile", d.profile, "contact", "+search-user", "--query", query, "--as", "user"); err != nil {
			return err
		}
		page := directoryPage{People: []DirectoryPerson{}, HasMore: response.Data.HasMore, At: time.Now()}
		d.mu.Lock()
		defer d.mu.Unlock()
		if d.userCache.Queries == nil {
			d.userCache.Queries = map[string]directoryPage{}
			d.userCache.People = map[string]cachedDirectoryPerson{}
		}
		seen := map[string]string{}
		for _, u := range response.Data.Users {
			if u.IsCrossTenant || (u.IsActivated != nil && !*u.IsActivated) {
				continue
			}
			address := u.EnterpriseEmail
			if address == "" {
				address = u.Email
			}
			address = directoryEmail(address)
			if address == "" {
				continue
			}
			previous := d.userCache.People[address]
			p := DirectoryPerson{Email: address, Name: u.LocalizedName, Department: u.Department}
			if previous.OpenID == u.OpenID {
				p.AvatarURL = previous.Person.AvatarURL
			} else {
				previous.AvatarAt = time.Time{}
			}
			page.People = append(page.People, p)
			ambiguous := seen[address] != "" && (seen[address] != u.OpenID || previous.Ambiguous)
			seen[address] = u.OpenID
			d.userCache.People[address] = cachedDirectoryPerson{Ambiguous: ambiguous, Person: p, OpenID: u.OpenID, At: page.At, AvatarAt: previous.AvatarAt}
		}
		d.userCache.Queries[query] = page
		d.saveUserCache()
		return nil
	})
	if err != nil {
		return directoryPage{}, err
	}
	p, _ := d.cachedPage(query)
	return p, nil
}

func (d *Directory) SearchPage(ctx context.Context, query string) ([]DirectoryPerson, bool, error) {
	if d.identity == "user" {
		p, err := d.searchUserCached(ctx, query)
		return p.People, p.HasMore, err
	}
	p, err := d.Search(ctx, query)
	return p, false, err
}

// Bulk page avatars never trigger a directory search per historical owner.
// Only identities already verified by a recent search can fetch a missing photo.
func (d *Directory) userAvatar(ctx context.Context, email string) (DirectoryPerson, error) {
	d.mu.Lock()
	p, ok := d.userCache.People[email]
	d.mu.Unlock()
	if !ok || p.Ambiguous || time.Since(p.At) > directoryTTL {
		return DirectoryPerson{Email: email}, nil
	}
	if time.Since(p.AvatarAt) < 24*time.Hour {
		return p.Person, nil
	}
	err := d.coalesce(ctx, "avatar:"+email, func(ctx context.Context) error {
		d.mu.Lock()
		if d.avatarGate == nil {
			d.avatarGate = make(chan struct{}, 1)
		}
		gate := d.avatarGate
		d.mu.Unlock()
		select {
		case gate <- struct{}{}:
			defer func() { <-gate }()
		case <-ctx.Done():
			return ctx.Err()
		}
		d.mu.Lock()
		p := d.userCache.People[email]
		d.mu.Unlock()
		if time.Since(p.AvatarAt) < 24*time.Hour {
			return nil
		}
		params, _ := json.Marshal(map[string]any{"query": email, "page_size": 20})
		var photos searchUserAvatarResponse
		if err := d.client.Run(ctx, &photos, "--profile", d.profile, "api", "GET", "/open-apis/search/v1/user", "--params", string(params), "--as", "user"); err != nil {
			return err
		}
		for _, photo := range photos.Data.Users {
			if photo.OpenID == p.OpenID {
				p.Person.AvatarURL = photo.Avatar.Medium
			}
		}
		p.AvatarAt = time.Now()
		d.mu.Lock()
		current := d.userCache.People[email]
		if current.OpenID == p.OpenID {
			current.Person.AvatarURL = p.Person.AvatarURL
			current.AvatarAt = p.AvatarAt
			d.userCache.People[email] = current
		}
		d.saveUserCache()
		d.mu.Unlock()
		return nil
	})
	d.mu.Lock()
	p = d.userCache.People[email]
	d.mu.Unlock()
	return p.Person, err
}
