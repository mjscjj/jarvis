package insight

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// WorklogService serves the Progress page's two work-log tabs:
//   - 今天的文档: 我今天写/编辑的飞书文档（bytedcli lark docs search，按 owner=@me）
//   - 我今天收到的文档（库里 M1 采集的 Resource，带 doc_token 的）。
//   - 项目代码: 我今天在各仓库的 MR（bytedcli codebase search mr --author @me）。
//
// 纯只读、按需实时查，不进 cron、不落库。外部数据源用本地可信的 bytedcli 直接
// exec 调用；命令失败即 fail-fast 把 stderr 抛给前端，不做静默兜底。
type WorklogService struct {
	db       *gorm.DB
	location *time.Location
	// bytedcliBin 是 bytedcli 可执行文件路径，默认 "bytedcli"（走 PATH）。
	bytedcliBin string
	// timeout 单条外部命令超时。飞书搜索/跨仓 MR 查询偶发慢，给足余量。
	timeout time.Duration
}

func NewWorklogService(db *gorm.DB, location *time.Location) (*WorklogService, error) {
	if db == nil {
		return nil, fmt.Errorf("worklog service db is nil")
	}
	if location == nil {
		return nil, fmt.Errorf("worklog service location is nil")
	}
	return &WorklogService{
		db:          db,
		location:    location,
		bytedcliBin: "bytedcli",
		timeout:     40 * time.Second,
	}, nil
}

// dayRange 返回 date（YYYY-MM-DD，本地时区）当天的 [start, end)。date 为空取今天。
func (s *WorklogService) dayRange(date string) (start, end time.Time, label string, err error) {
	var day time.Time
	if strings.TrimSpace(date) == "" {
		now := time.Now().In(s.location)
		day = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, s.location)
	} else {
		day, err = time.ParseInLocation("2006-01-02", strings.TrimSpace(date), s.location)
		if err != nil {
			return time.Time{}, time.Time{}, "", fmt.Errorf("invalid date %q, want YYYY-MM-DD: %w", date, err)
		}
	}
	return day, day.AddDate(0, 0, 1), day.Format("2006-01-02"), nil
}

// runBytedcli 执行一条 bytedcli 命令并返回 stdout。失败时把 stderr 一并带出，
// 便于前端直接看到「未登录 / 网络 / 权限」等真实原因。
func (s *WorklogService) runBytedcli(ctx context.Context, args ...string) ([]byte, error) {
	cctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, s.bytedcliBin, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("bytedcli %s failed: %v: %s", strings.Join(args, " "), err, detail)
	}
	return []byte(stdout.String()), nil
}

// ---------------- 项目代码（MR）----------------

// CommitMR 是我在某个仓库的一条 MR（提交入口以 MR 为粒度，符合字节 protected-branch
// 走 MR 的实际流程）。
type CommitMR struct {
	Title           string  `json:"title"`
	URL             string  `json:"url"`
	Status          string  `json:"status"` // open | merged | closed
	CommitsCount    int     `json:"commits_count"`
	ChangesCount    int     `json:"changes_count"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
	MergedAt        *string `json:"merged_at"`
	TargetBranch    string  `json:"target_branch"`
	CheckRunSummary string  `json:"check_run_summary"`
}

// CommitRepo 把我的 MR 按仓库归组。
type CommitRepo struct {
	Repo string     `json:"repo"` // 形如 team/jarvis_bot，从 URL 解析
	MRs  []CommitMR `json:"mrs"`
}

// CommitWorklog 是「项目代码」Tab 的载荷。
type CommitWorklog struct {
	Date  string       `json:"date"`
	Repos []CommitRepo `json:"repos"`
}

// mrSearchEnvelope 对齐 `bytedcli --json codebase search mr` 的输出。
type mrSearchEnvelope struct {
	Status string `json:"status"`
	Data   struct {
		MergeRequests []mrRaw `json:"merge_requests"`
	} `json:"data"`
	Error json.RawMessage `json:"error"`
}

type mrRaw struct {
	Title            string  `json:"Title"`
	Status           string  `json:"Status"`
	URL              string  `json:"URL"`
	TargetBranchName string  `json:"TargetBranchName"`
	CommitsCount     int     `json:"CommitsCount"`
	ChangesCount     int     `json:"ChangesCount"`
	CreatedAt        string  `json:"CreatedAt"`
	UpdatedAt        string  `json:"UpdatedAt"`
	MergedAt         *string `json:"MergedAt"`
	CheckRunSummary  string  `json:"CheckRunSummaryStatus"`
}

// Commits 查询我在 date 当天有更新的 MR，按仓库归组。用 --updated-since 让服务端
// 只回当天窗口内的，避免拉全量再本地过滤。
func (s *WorklogService) Commits(ctx context.Context, date string) (*CommitWorklog, error) {
	start, end, label, err := s.dayRange(date)
	if err != nil {
		return nil, err
	}
	out, err := s.runBytedcli(ctx,
		"--json", "codebase", "search", "mr",
		"--author", "@me",
		"--sort-by", "UpdatedAt", "--sort-order", "Desc",
		"--updated-since", start.Format(time.RFC3339),
		"--updated-until", end.Format(time.RFC3339),
		"--page-size", "100",
	)
	if err != nil {
		return nil, err
	}
	var env mrSearchEnvelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("decode codebase mr search: %w", err)
	}
	if env.Status != "" && env.Status != "success" {
		return nil, fmt.Errorf("codebase mr search returned status=%q error=%s", env.Status, string(env.Error))
	}

	byRepo := map[string][]CommitMR{}
	order := make([]string, 0)
	for i := range env.Data.MergeRequests {
		raw := &env.Data.MergeRequests[i]
		repo := repoFromMRURL(raw.URL)
		if _, ok := byRepo[repo]; !ok {
			order = append(order, repo)
		}
		byRepo[repo] = append(byRepo[repo], CommitMR{
			Title:           raw.Title,
			URL:             raw.URL,
			Status:          strings.ToLower(raw.Status),
			CommitsCount:    raw.CommitsCount,
			ChangesCount:    raw.ChangesCount,
			CreatedAt:       raw.CreatedAt,
			UpdatedAt:       raw.UpdatedAt,
			MergedAt:        raw.MergedAt,
			TargetBranch:    raw.TargetBranchName,
			CheckRunSummary: raw.CheckRunSummary,
		})
	}
	repos := make([]CommitRepo, 0, len(order))
	for _, repo := range order {
		repos = append(repos, CommitRepo{Repo: repo, MRs: byRepo[repo]})
	}
	return &CommitWorklog{Date: label, Repos: repos}, nil
}

// repoFromMRURL 从 MR 的 console URL 里抠出 owner/repo。
// 形如 https://code.byted.org/team/jarvis_bot/merge_requests/1 → team/jarvis_bot。
func repoFromMRURL(url string) string {
	const marker = "/merge_requests/"
	idx := strings.Index(url, marker)
	if idx < 0 {
		return "unknown"
	}
	path := url[:idx]
	// 去掉协议和 host，留 owner/repo。
	if i := strings.Index(path, "://"); i >= 0 {
		path = path[i+3:]
	}
	if i := strings.Index(path, "/"); i >= 0 {
		path = path[i+1:] // 去掉 host
	}
	if path == "" {
		return "unknown"
	}
	return path
}

// ---------------- 今天的文档 ----------------

// WorkDoc 是一条文档（我写的或我收到的）。
type WorkDoc struct {
	Title    string `json:"title"`
	URL      string `json:"url"`
	DocType  string `json:"doc_type"`            // DOCX/SHEET/WIKI/... 或采集侧的 resource_type
	Time     string `json:"time"`                // ISO8601：我写的取编辑时间，我收到的取采集时间
	FromWho  string `json:"from_who,omitempty"`  // 「我收到的」：发送人
	FromChat string `json:"from_chat,omitempty"` // 「我收到的」：来源群
}

// DocumentWorklog 是「今天的文档」Tab 的载荷。
type DocumentWorklog struct {
	Date     string    `json:"date"`
	Authored []WorkDoc `json:"authored"` // 我今天写/编辑的
	Received []WorkDoc `json:"received"` // 我今天收到的（采集自消息）
}

// docSearchEnvelope 对齐 `bytedcli --json lark docs search`。
type docSearchEnvelope struct {
	OK   bool `json:"ok"`
	Data struct {
		Results []docResult `json:"results"`
	} `json:"data"`
	Error json.RawMessage `json:"error"`
}

type docResult struct {
	EntityType string `json:"entity_type"`
	Meta       struct {
		DocTypes      string `json:"doc_types"`
		OwnerName     string `json:"owner_name"`
		EditUserName  string `json:"edit_user_name"`
		URL           string `json:"url"`
		UpdateTimeISO string `json:"update_time_iso"`
		CreateTimeISO string `json:"create_time_iso"`
		LastOpenISO   string `json:"last_open_time_iso"`
	} `json:"result_meta"`
	Title string `json:"title_highlighted"`
}

// Documents 拼「我今天写的」（飞书搜索 owner=@me，按编辑时间落在当天过滤）与
// 「我今天收到的」（库里当天采集、带 doc_token 的 Resource）。飞书搜索失败即
// fail-fast 报错（用户明确要求这块必须打通，不静默降级）。
func (s *WorklogService) Documents(ctx context.Context, date string) (*DocumentWorklog, error) {
	start, end, label, err := s.dayRange(date)
	if err != nil {
		return nil, err
	}
	result := &DocumentWorklog{Date: label, Authored: []WorkDoc{}, Received: []WorkDoc{}}

	authored, err := s.authoredDocs(ctx, start, end)
	if err != nil {
		return nil, err
	}
	result.Authored = authored

	received, err := s.receivedDocs(ctx, start, end)
	if err != nil {
		return nil, err
	}
	result.Received = received
	return result, nil
}

// authoredDocs 调飞书文档搜索拿 owner=我 的文档，保留编辑时间落在 [start,end) 的。
// 搜索接口需要一个 query，用宽泛空格；再按时间窗口本地过滤。
func (s *WorklogService) authoredDocs(ctx context.Context, start, end time.Time) ([]WorkDoc, error) {
	out, err := s.runBytedcli(ctx,
		"--json", "lark", "docs", "search",
		"--as", "user",
		"--query", " ",
		"--filter", `{"owner_ids":["@me"]}`,
		"--page-size", "20",
	)
	if err != nil {
		return nil, err
	}
	var env docSearchEnvelope
	if err := json.Unmarshal(out, &env); err != nil {
		return nil, fmt.Errorf("decode lark docs search: %w", err)
	}
	if !env.OK {
		return nil, fmt.Errorf("lark docs search failed: %s", string(env.Error))
	}
	docs := make([]WorkDoc, 0)
	for i := range env.Data.Results {
		r := &env.Data.Results[i]
		edited := parseISOTime(r.Meta.UpdateTimeISO, s.location)
		if edited.IsZero() || edited.Before(start) || !edited.Before(end) {
			continue
		}
		docs = append(docs, WorkDoc{
			Title:   stripHighlight(r.Title),
			URL:     r.Meta.URL,
			DocType: docTypeLabel(r.EntityType, r.Meta.DocTypes),
			Time:    r.Meta.UpdateTimeISO,
		})
	}
	return docs, nil
}

// receivedDocs 查库里当天采集、带 doc_token（真正的飞书文档）的 Resource。
func (s *WorklogService) receivedDocs(ctx context.Context, start, end time.Time) ([]WorkDoc, error) {
	var rows []domain.Resource
	if err := s.db.WithContext(ctx).
		Where("doc_token IS NOT NULL AND doc_token <> ''").
		Where("created_at >= ? AND created_at < ?", start, end).
		Order("created_at DESC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("query received docs: %w", err)
	}
	// 来源群名一次性查出来，避免逐条查询。
	groupNames := s.groupNameLookup(ctx, rows)
	docs := make([]WorkDoc, 0, len(rows))
	for i := range rows {
		r := &rows[i]
		doc := WorkDoc{
			DocType: r.ResourceType,
			Time:    r.CreatedAt.In(s.location).Format(time.RFC3339),
		}
		if r.Name != nil {
			doc.Title = *r.Name
		}
		if r.URL != nil {
			doc.URL = *r.URL
		}
		if doc.URL == "" && r.DocToken != nil {
			doc.URL = "https://bytedance.larkoffice.com/docx/" + *r.DocToken
		}
		if r.GroupID != nil {
			doc.FromChat = groupNames[*r.GroupID]
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

// groupNameLookup 批量把 group_id 映射到群名（取不到就用 chat_id）。
func (s *WorklogService) groupNameLookup(ctx context.Context, rows []domain.Resource) map[uint64]string {
	ids := make([]uint64, 0)
	seen := map[uint64]struct{}{}
	for i := range rows {
		if rows[i].GroupID == nil {
			continue
		}
		id := *rows[i].GroupID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	out := map[uint64]string{}
	if len(ids) == 0 {
		return out
	}
	var groups []domain.Group
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&groups).Error; err != nil {
		// 群名只是展示增强，查不到不阻断主流程；空名前端会退回展示为空。
		return out
	}
	for i := range groups {
		g := &groups[i]
		name := g.ChatID
		if g.Name != nil && *g.Name != "" {
			name = *g.Name
		}
		out[g.ID] = name
	}
	return out
}

func parseISOTime(s string, loc *time.Location) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.In(loc)
}

// stripHighlight 去掉飞书搜索标题里的高亮标签（<b>/<h>/<hb> 等）。
func stripHighlight(s string) string {
	replacer := strings.NewReplacer(
		"<b>", "", "</b>", "",
		"<h>", "", "</h>", "",
		"<hb>", "", "</hb>", "",
	)
	return strings.TrimSpace(replacer.Replace(s))
}

func docTypeLabel(entityType, docTypes string) string {
	if strings.TrimSpace(docTypes) != "" {
		return docTypes
	}
	return entityType
}
