package extract

import (
	"strings"

	"jarvis/internal/contextsnap"
)

// resolveProject decides which project a candidate belongs to and produces the
// resolution trace (docs/design-context-pipeline.md §2.1/§2.2).
//
// Priority:
//  1. group-bound project — highest priority signal (MethodGroupBound)
//  2. candidate.project_hint matched against a known project code/name
//     (MethodProjectHint) — this is also how codex expresses a CLI-inferred
//     project, since it writes its conclusion back into project_hint
//  3. otherwise unresolved (MethodUnresolved)
//
// The returned *uint64 is authoritative for the Todo column AND the dedup
// fingerprint. reposHint is carried through when the matched project has none of
// our repo data yet (repos are not populated this round).
func resolveProject(batch ChatBatch, candidate Candidate) (*uint64, contextsnap.Resolution) {
	return resolveProjectByHint(batch, candidate.ProjectHint)
}

// resolveProjectByHint attributes a candidate to a project: a group binding
// wins outright, otherwise the model's hint is matched against known projects.
func resolveProjectByHint(batch ChatBatch, projectHint *string) (*uint64, contextsnap.Resolution) {
	if batch.Group.ProjectID != nil {
		name := boundProjectName(batch)
		res := contextsnap.Resolution{
			Method:      contextsnap.MethodGroupBound,
			ProjectID:   copyUint64(batch.Group.ProjectID),
			ProjectName: nonEmptyPtr(name),
			Confidence:  1.0,
			Basis:       "群已绑定项目，直接继承为最高优先级信号",
		}
		return copyUint64(batch.Group.ProjectID), res
	}

	hint := ""
	if projectHint != nil {
		hint = strings.TrimSpace(*projectHint)
	}
	if hint != "" {
		if id, name, ok := matchProjectByHint(batch, hint); ok {
			res := contextsnap.Resolution{
				Method:      contextsnap.MethodProjectHint,
				ProjectID:   &id,
				ProjectName: nonEmptyPtr(name),
				Confidence:  0.8,
				Basis:       "project_hint=\"" + hint + "\" 匹配到已知项目 code/name",
			}
			return &id, res
		}
	}

	res := contextsnap.Resolution{
		Method:     contextsnap.MethodUnresolved,
		Confidence: 0,
		Basis:      "群未绑定项目，且 project_hint 未匹配到任何已知项目：hint=\"" + hint + "\"",
	}
	return nil, res
}

// matchProjectByHint matches a hint against every known project's code (exact,
// case-insensitive) then name (exact, case-insensitive). Bound project is not
// present here (this path only runs when the group is unbound), so only
// OtherProjects are searched.
func matchProjectByHint(batch ChatBatch, hint string) (uint64, string, bool) {
	folded := strings.ToLower(hint)
	for _, project := range batch.OtherProjects {
		if project.Code != "" && strings.ToLower(project.Code) == folded {
			return project.ID, project.Name, true
		}
	}
	for _, project := range batch.OtherProjects {
		if strings.ToLower(strings.TrimSpace(project.Name)) == folded {
			return project.ID, project.Name, true
		}
	}
	return 0, "", false
}

func boundProjectName(batch ChatBatch) string {
	if batch.Project != nil {
		return batch.Project.Name
	}
	return ""
}

func nonEmptyPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
