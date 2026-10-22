package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestLabelsCellWraps(t *testing.T) {
	labels := []Label{
		{"platform::android", "#009966", "#FFFFFF"},
		{"priority::next", "#eee600", "#1F1E24"},
		{"claude-review::all-clear", "#428BCA", "#FFFFFF"},
		{"a-label-name-that-is-far-too-long-for-the-column", "#000000", "#FFFFFF"},
	}
	const width = 30
	out := labelsCell(labels, width, true)
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("expected labels to wrap onto several lines, got %d:\n%s", len(lines), out)
	}
	for _, l := range lines {
		if w := lipgloss.Width(l); w > width {
			t.Errorf("line is %d wide, max %d: %q", w, width, l)
		}
	}
	one := labelsCell(labels, width, false)
	if strings.Contains(one, "\n") || lipgloss.Width(one) > width || !strings.HasSuffix(ansi.Strip(one), "+3") {
		t.Errorf("unwrapped labels should be one line ending in +3, got %q", ansi.Strip(one))
	}
	// A first label that only just fits must shrink to leave room for the count.
	tight := labelsCell([]Label{{"autoreview::running", "#428BCA", "#FFFFFF"}, {"ux", "#9400D3", "#FFFFFF"}}, 23, false)
	if strings.Contains(tight, "\n") || lipgloss.Width(tight) > 23 || !strings.HasSuffix(ansi.Strip(tight), "+1") {
		t.Errorf("tight labels should be one line of at most 23 ending in +1, got %q (%d wide)", ansi.Strip(tight), lipgloss.Width(tight))
	}
	if got := labelsCell(nil, width, false); lipgloss.Width(got) != 1 {
		t.Errorf("no labels should render as a dash, got %q", got)
	}
}

func TestSplash(t *testing.T) {
	update := func(m model, msg tea.Msg) model {
		next, _ := m.Update(msg)
		return next.(model)
	}
	start := func() model {
		return update(newModel(Config{Host: "gitlab.com"}, nil), tea.WindowSizeMsg{Width: 100, Height: 30})
	}
	shows := func(m model) bool { return strings.Contains(ansi.Strip(m.View()), "████") }

	// A quick first load keeps the splash up until splashMin has passed.
	m := start()
	if !shows(m) {
		t.Fatal("splash not shown at start")
	}
	m = update(m, mrsMsg{})
	if !shows(m) {
		t.Error("splash gone before splashMin")
	}
	if m = update(m, splashDoneMsg{}); shows(m) {
		t.Error("splash still shown after splashMin and the first load")
	}

	// A slow first load keeps it up until the load ends.
	m = update(start(), splashDoneMsg{})
	if !shows(m) {
		t.Error("splash gone while the first load is running")
	}
	if m = update(m, mrsMsg{}); shows(m) {
		t.Error("splash still shown after the first load")
	}

	// Any key skips it.
	if m = update(start(), tea.KeyMsg{Type: tea.KeyEsc}); shows(m) {
		t.Error("splash still shown after a key press")
	}

	// A narrow window gets a plain title instead of the logo.
	m = update(newModel(Config{Host: "gitlab.com"}, nil), tea.WindowSizeMsg{Width: 30, Height: 10})
	if v := ansi.Strip(m.View()); !strings.Contains(v, "tglab") || strings.Contains(v, "████") {
		t.Errorf("narrow splash:\n%s", v)
	}
}

func TestCornerLogo(t *testing.T) {
	lines := []string{"", "updated 12:00:00", "q quit"}
	got, ok := withCornerLogo(lines, 40)
	if !ok || !strings.HasPrefix(got[2], "q quit") || !strings.HasSuffix(ansi.Strip(got[2]), "┗━┛") || lipgloss.Width(got[2]) != 39 {
		t.Errorf("logo not at the right of the lines: %q", got)
	}
	// A line too long to share with the logo leaves them all unchanged.
	if got, ok := withCornerLogo([]string{"", strings.Repeat("x", 25), ""}, 40); ok || strings.Join(got, "") != strings.Repeat("x", 25) {
		t.Errorf("logo drawn over a long line: %q", got)
	}
}
