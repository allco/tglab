package main

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestParseSort(t *testing.T) {
	for in, want := range map[string]sortOrder{
		"":             {},
		"updated":      {},
		"updated desc": {},
		"Updated ASC":  {0, true},
		"title":        {4, true},
		"title desc":   {4, false},
		"threads":      {7, false},
		"none":         {9, true},
	} {
		got, err := parseSort(in)
		if err != nil || got != want {
			t.Errorf("parseSort(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"age", "title up", "title asc x", "none desc"} {
		if _, err := parseSort(in); err == nil {
			t.Errorf("parseSort(%q) should fail", in)
		}
	}
}

func TestSortKeys(t *testing.T) {
	now := time.Now()
	mk := func(iid, title string, age time.Duration, threads int, status string) MR {
		mr := MR{IID: iid, Title: title, UpdatedAt: now.Add(-age), DetailedMergeStatus: status}
		mr.ResolvableThreads = threads
		return mr
	}
	fetched := []MR{
		mk("9", "beta", 3*time.Hour, 0, "MERGEABLE"),
		mk("10", "Alpha", time.Hour, 2, "NOT_APPROVED"),
		mk("2", "gamma", 2*time.Hour, 1, "DRAFT_STATUS"),
	}
	order := func(o string) string {
		s, err := parseSort(o)
		if err != nil {
			t.Fatal(err)
		}
		mrs := append([]MR(nil), fetched...)
		sortMRs(mrs, s)
		var ids []string
		for _, mr := range mrs {
			ids = append(ids, mr.IID)
		}
		return strings.Join(ids, " ")
	}
	for in, want := range map[string]string{
		"updated":     "10 2 9",
		"updated asc": "9 2 10",
		"mr":          "10 9 2", // numeric, not as text
		"title":       "10 9 2", // ignoring case
		"threads":     "10 2 9",
		"status":      "10 9 2", // needs approval, ready, draft
		"status desc": "2 9 10",
		"none":        "9 10 2",
	} {
		if got := order(in); got != want {
			t.Errorf("sort %q: got %s, want %s", in, got, want)
		}
	}
}

func TestSortKeysInUI(t *testing.T) {
	update := func(m model, msg tea.Msg) model {
		next, _ := m.Update(msg)
		return next.(model)
	}
	key := func(m model, k string) model { return update(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}) }
	now := time.Now()
	mrs := []MR{
		{IID: "1", WebURL: "u1", Title: "b", UpdatedAt: now.Add(-time.Hour)},
		{IID: "2", WebURL: "u2", Title: "a", UpdatedAt: now},
	}
	for i := range mrs {
		mrs[i].Author.Username = "me"
	}
	m := newModel(Config{Host: "gitlab.com", Projects: []string{"g/p"}}, nil)
	m = update(m, tea.WindowSizeMsg{Width: 140, Height: 20})
	m = update(m, splashDoneMsg{})
	m = update(m, mrsMsg{mrs: mrs, me: "me"})
	first := func(m model) string { return m.mrs[0].IID }
	header := func(m model) string { return ansi.Strip(strings.Split(m.View(), "\n")[1]) }

	if first(m) != "2" || !strings.Contains(header(m), "Updated▼") {
		t.Fatalf("default should be newest first with Updated▼, got first !%s, header %q", first(m), header(m))
	}
	m.cursor = 1 // !1
	m = key(m, "S")
	if first(m) != "1" || !strings.Contains(header(m), "Updated▲") || m.mrs[m.cursor].IID != "1" {
		t.Fatalf("S should reverse and keep the selection, got first !%s, header %q", first(m), header(m))
	}
	// Project and Author are hidden here (one project, my MRs), so s skips them.
	m = key(m, "s")
	m = key(m, "s")
	if sortKeys[m.sort.key].name != "title" || first(m) != "2" || !strings.Contains(header(m), "Title ▲") {
		t.Fatalf("s twice should sort by title ascending, got %s, first !%s", sortKeys[m.sort.key].name, first(m))
	}
	for sortKeys[m.sort.key].name != "none" {
		m = key(m, "s")
	}
	if first(m) != "1" || strings.ContainsAny(header(m), "▲▼") || !strings.Contains(ansi.Strip(m.View()), "unsorted") {
		t.Fatalf("none should keep the fetched order without an arrow, got first !%s, header %q", first(m), header(m))
	}
	if m = key(m, "s"); sortKeys[m.sort.key].name != "updated" {
		t.Fatalf("s after none should wrap to updated, got %s", sortKeys[m.sort.key].name)
	}
}
