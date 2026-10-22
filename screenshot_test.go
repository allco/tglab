package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestScreenshots renders the app with made-up MRs and writes each screen as ANSI text to
// $TGLAB_SCREENSHOTS. scripts/screenshots.sh turns those into the PNGs in docs/screenshots.
func TestScreenshots(t *testing.T) {
	dir := os.Getenv("TGLAB_SCREENSHOTS")
	if dir == "" {
		t.Skip("set TGLAB_SCREENSHOTS to a directory to render screenshots")
	}
	lipgloss.SetColorProfile(termenv.TrueColor)

	cfg := Config{Host: "gitlab.com", Projects: []string{"acme/mobile-app"}, RefreshInterval: time.Minute}
	shots := []struct {
		name   string
		keys   string
		all    bool
		height int
	}{
		{name: "my-mrs", height: 11},
		{name: "selected", keys: "jjjjj", height: 13}, // selects !398, whose labels overflow
		{name: "all-mrs", all: true, height: 15},
	}
	for _, shot := range shots {
		m := newModel(cfg, nil)
		m.loading = false
		m.splash = false
		m.me = "jane.doe"
		m.all = shot.all
		next, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: shot.height})
		m = next.(model)

		before, after := demoMRs()
		m.applyMRs(before)
		m.applyMRs(after) // the second load marks what changed with ●
		m.lastUpdate = time.Date(2026, 10, 22, 14, 32, 5, 0, time.Local)

		for _, k := range shot.keys {
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
			m = next.(model)
		}
		if err := os.WriteFile(filepath.Join(dir, shot.name+".ansi"), []byte(m.View()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// demoMRs returns two consecutive loads of made-up MRs; the second has a few changes.
func demoMRs() (before, after []MR) {
	label := func(title, bg, fg string) Label { return Label{Title: title, Color: bg, TextColor: fg} }
	review := map[string]Label{
		"clean":   label("autoreview::clean", "#2DA160", "#FFFFFF"),
		"fixing":  label("autoreview::worth-fixing", "#E9BE74", "#1F1E24"),
		"running": label("autoreview::running", "#428BCA", "#FFFFFF"),
	}
	bug := label("bug", "#DC143C", "#FFFFFF")
	feature := label("feature", "#6699CC", "#FFFFFF")
	ux := label("ux", "#9400D3", "#FFFFFF")
	perf := label("performance", "#CC5500", "#FFFFFF")

	type spec struct {
		iid, title, author, pipeline, merge string
		draft, approved, conflicts          bool
		approvals, threads                  int
		train                               *int
		autoMerge                           string
		age                                 time.Duration
		labels                              []Label
	}
	one, two := 0, 1
	specs := []spec{
		{iid: "412", title: "Add offline mode to the settings screen", author: "jane.doe", pipeline: "SUCCESS", approved: true, approvals: 1, train: &two, age: 4 * time.Minute, labels: []Label{review["clean"], feature}},
		{iid: "418", title: "Cache map tiles on disk so the map opens instantly after a restart", author: "sam.lee", pipeline: "SUCCESS", approved: true, approvals: 1, train: &one, age: 6 * time.Minute, labels: []Label{review["clean"], perf}},
		{iid: "409", title: "Draft: Migrate the login flow to the new auth service", author: "jane.doe", pipeline: "FAILED", merge: "DRAFT_STATUS", draft: true, threads: 1, age: 9 * time.Minute, labels: []Label{review["fixing"]}},
		{iid: "415", title: "Show a retry button when search fails", author: "jane.doe", pipeline: "RUNNING", merge: "CI_STILL_RUNNING", age: 12 * time.Minute, labels: []Label{review["running"], ux}},
		{iid: "416", title: "Fix the badge count after notifications are cleared", author: "alex.kim", pipeline: "SUCCESS", merge: "NOT_APPROVED", age: 35 * time.Minute, labels: []Label{bug}},
		{iid: "403", title: "Update the onboarding illustrations for dark mode", author: "jane.doe", pipeline: "SUCCESS", merge: "MERGEABLE", approved: true, approvals: 1, autoMerge: "", age: 2 * time.Hour, labels: []Label{review["clean"], ux}},
		{iid: "401", title: "Rename the analytics events to the new naming scheme", author: "priya.n", pipeline: "SUCCESS", conflicts: true, merge: "CONFLICT", approvals: 0, age: 5 * time.Hour},
		{iid: "398", title: "Fix crash when the map has no tiles", author: "jane.doe", pipeline: "SUCCESS", merge: "DISCUSSIONS_NOT_RESOLVED", approved: true, approvals: 1, threads: 2, age: 26 * time.Hour, labels: []Label{review["fixing"], bug}},
		{iid: "395", title: "Bump the Kotlin and Compose versions", author: "sam.lee", pipeline: "MANUAL", merge: "NOT_APPROVED", age: 3 * 24 * time.Hour},
	}

	now := time.Now()
	for _, s := range specs {
		mr := MR{
			IID:                 s.iid,
			Title:               s.title,
			WebURL:              "https://gitlab.com/acme/mobile-app/-/merge_requests/" + s.iid,
			Draft:               s.draft,
			UpdatedAt:           now.Add(-s.age),
			Approved:            s.approved,
			ApprovalsRequired:   1,
			ApprovalsLeft:       1 - s.approvals,
			Conflicts:           s.conflicts,
			DetailedMergeStatus: s.merge,
			MergeTrainIndex:     s.train,
			AutoMergeEnabled:    s.train != nil,
			AutoMergeStrategy:   "merge_train",
			ResolvableThreads:   s.threads,
		}
		if s.train == nil {
			mr.AutoMergeStrategy = s.autoMerge
		}
		mr.Author.Username = s.author
		mr.Project.FullPath = "acme/mobile-app"
		mr.HeadPipeline = &struct {
			Status string `json:"status"`
		}{Status: s.pipeline}
		mr.Labels.Nodes = s.labels
		after = append(after, mr)
	}
	sortLabels(after)

	// The previous load differs in the pipeline of !409 and the review label of !415.
	before = make([]MR, len(after))
	for i, mr := range after {
		before[i] = mr
		before[i].Labels.Nodes = append([]Label(nil), mr.Labels.Nodes...)
		switch mr.IID {
		case "409":
			before[i].HeadPipeline = &struct {
				Status string `json:"status"`
			}{Status: "RUNNING"}
		case "415":
			before[i].Labels.Nodes = []Label{ux}
		}
	}
	return before, after
}
