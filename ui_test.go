package main

import (
	"strings"
	"testing"

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
	if got := labelsCell(nil, width, false); lipgloss.Width(got) != 1 {
		t.Errorf("no labels should render as a dash, got %q", got)
	}
}
