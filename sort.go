package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// sortKey is one way to order the rows. cmp orders a before b in ascending (▲) order; desc is the
// direction a key starts in, the one that is usually wanted. The none key has no cmp: the rows stay
// in the order GitLab sent them.
type sortKey struct {
	name   string // as written in the settings file
	column string // the column header it marks
	desc   bool
	cmp    func(a, b MR) int
}

// sortKeys in the order `s` cycles through them.
var sortKeys = []sortKey{
	{"updated", "Updated", true, func(a, b MR) int { return a.UpdatedAt.Compare(b.UpdatedAt) }},
	{"mr", "MR", true, func(a, b MR) int { return cmpInt(iidNumber(a), iidNumber(b)) }},
	{"project", "Project", false, func(a, b MR) int { return cmpFold(a.Project.FullPath, b.Project.FullPath) }},
	{"author", "Author", false, func(a, b MR) int { return cmpFold(a.Author.Username, b.Author.Username) }},
	{"title", "Title", false, func(a, b MR) int { return cmpFold(a.Title, b.Title) }},
	{"pipeline", "Pipeline", false, func(a, b MR) int {
		return cmpInt(pipelineRank(a.PipelineStatus()), pipelineRank(b.PipelineStatus()))
	}},
	{"approvals", "Approvals", true, func(a, b MR) int { return cmpInt(approvalsMissing(a), approvalsMissing(b)) }},
	{"threads", "Threads", true, func(a, b MR) int { return cmpInt(a.UnresolvedThreads(), b.UnresolvedThreads()) }},
	{"status", "Status", false, func(a, b MR) int { return cmpInt(statusRank(statusCell(a)), statusRank(statusCell(b))) }},
	{"none", "", false, nil},
}

// sortOrder is the current key (an index into sortKeys) and direction. The zero value is the
// default: most recently updated first.
type sortOrder struct {
	key int
	asc bool
}

// parseSort reads the `sort` setting: a key name, optionally followed by asc or desc, or none.
func parseSort(s string) (sortOrder, error) {
	f := strings.Fields(strings.ToLower(s))
	if len(f) == 0 {
		return sortOrder{}, nil
	}
	if len(f) > 2 {
		return sortOrder{}, fmt.Errorf("sort: %q: want a column and optionally asc or desc", s)
	}
	for i, k := range sortKeys {
		if k.name != f[0] {
			continue
		}
		o := sortOrder{i, !k.desc}
		if len(f) == 2 && k.cmp == nil {
			return sortOrder{}, fmt.Errorf("sort: %q: none takes no direction", s)
		}
		if len(f) == 2 {
			switch f[1] {
			case "asc":
				o.asc = true
			case "desc":
				o.asc = false
			default:
				return sortOrder{}, fmt.Errorf("sort: %q: direction must be asc or desc", s)
			}
		}
		return o, nil
	}
	names := make([]string, len(sortKeys))
	for i, k := range sortKeys {
		names[i] = k.name
	}
	return sortOrder{}, fmt.Errorf("sort: unknown column %q, want one of %s", f[0], strings.Join(names, ", "))
}

// sortMRs orders mrs in place. Ties go to the most recently updated MR.
func sortMRs(mrs []MR, o sortOrder) {
	k := sortKeys[o.key]
	if k.cmp == nil {
		return
	}
	slices.SortStableFunc(mrs, func(a, b MR) int {
		c := k.cmp(a, b)
		if !o.asc {
			c = -c
		}
		if c == 0 {
			c = b.UpdatedAt.Compare(a.UpdatedAt)
		}
		return c
	})
}

func cmpInt(a, b int) int { return a - b }

func cmpFold(a, b string) int { return strings.Compare(strings.ToLower(a), strings.ToLower(b)) }

func iidNumber(mr MR) int {
	n, _ := strconv.Atoi(mr.IID)
	return n
}

func approvalsMissing(mr MR) int {
	if mr.Approved {
		return 0
	}
	return mr.ApprovalsLeft
}

// pipelineRank puts what needs looking at first: failed, then running, waiting and finished.
func pipelineRank(s string) int {
	switch s {
	case "FAILED":
		return 0
	case "CANCELED", "CANCELING":
		return 1
	case "MANUAL":
		return 2
	case "RUNNING":
		return 3
	case "PENDING", "CREATED", "WAITING_FOR_RESOURCE", "PREPARING", "SCHEDULED", "WAITING_FOR_CALLBACK":
		return 4
	case "SUCCESS":
		return 5
	case "SKIPPED":
		return 6
	case "":
		return 8
	}
	return 7
}

// statusRank follows statusColor's groups: broken, needs action, waiting on CI, ready, merging on
// its own, not ready yet.
func statusRank(status string) int {
	switch {
	case status == "conflicts", status == "blocked":
		return 0
	case status == "needs rebase", status == "open threads", status == "needs approval":
		return 1
	case status == "waiting for CI":
		return 2
	case status == "ready to merge":
		return 3
	case strings.HasPrefix(status, "in train"), status == "train after CI", status == "auto-merge":
		return 4
	case status == "checking":
		return 5
	case status == "draft":
		return 7
	}
	return 6
}
