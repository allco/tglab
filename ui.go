package main

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("204"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	changeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	headerStyle   = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("229"))
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))

	pipelineColors = map[string]lipgloss.Color{
		"SUCCESS":  lipgloss.Color("42"),
		"FAILED":   lipgloss.Color("196"),
		"RUNNING":  lipgloss.Color("39"),
		"CANCELED": lipgloss.Color("241"),
		"SKIPPED":  lipgloss.Color("241"),
	}
)

const colGap = 2

type mrsMsg struct {
	mrs []MR
	me  string
	err error
}

type tickMsg struct{ gen int }

type column struct {
	title string
	width int
}

type model struct {
	cfg    Config
	client *Client

	spinner spinner.Model

	all     bool              // every author's MRs instead of just mine
	me      string            // current user's username
	bucket  []MR              // everything fetched; both views filter this, so switching needs no request
	mrs     []MR              // the rows shown: bucket filtered for the current view
	cursor  int               // selected row, -1 when nothing is selected
	offset  int               // first row shown
	prev    map[string]string // webUrl -> signature from the previous refresh
	changed map[string]bool   // MRs whose signature changed on the last refresh

	loading    bool
	gen        int // invalidates stale ticks after a manual refresh
	lastUpdate time.Time
	err        error
	width      int
	height     int
}

func newModel(cfg Config, client *Client) model {
	sp := spinner.New(spinner.WithSpinner(spinner.Dot))
	return model{cfg: cfg, client: client, spinner: sp, loading: true, cursor: -1, changed: map[string]bool{}}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(m.fetchCmd(), m.spinner.Tick)
}

func (m model) fetchCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		mrs, me, err := m.client.FetchMRs(ctx, m.cfg.Projects)
		for i := range mrs {
			mrs[i].Labels.Nodes = m.cfg.visibleLabels(mrs[i].Labels.Nodes)
		}
		return mrsMsg{mrs: mrs, me: me, err: err}
	}
}

func tickCmd(d time.Duration, gen int) tea.Cmd {
	return tea.Tick(d, func(time.Time) tea.Msg { return tickMsg{gen: gen} })
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scroll()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			m.cursor = -1
		case "a":
			if len(m.cfg.Projects) == 0 {
				m.err = fmt.Errorf("showing all MRs needs projects in the settings file")
				return m, nil
			}
			m.all = !m.all
			m.offset = 0
			m.refilter()
		case "r":
			if !m.loading {
				m.loading = true
				return m, tea.Batch(m.fetchCmd(), m.spinner.Tick)
			}
		case "up", "k":
			m.moveCursor(-1)
		case "down", "j":
			m.moveCursor(1)
		case "pgup":
			m.moveCursor(-5)
		case "pgdown":
			m.moveCursor(5)
		case "home", "g":
			m.moveCursor(-len(m.mrs))
		case "end", "G":
			m.moveCursor(len(m.mrs))
		case "enter", "o":
			if m.cursor >= 0 && m.cursor < len(m.mrs) {
				openBrowser(m.mrs[m.cursor].WebURL)
			}
		}
		return m, nil

	case mrsMsg:
		m.loading = false
		m.gen++
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.err = nil
			m.me = msg.me
			m.applyMRs(msg.mrs)
			m.lastUpdate = time.Now()
		}
		return m, tickCmd(m.cfg.RefreshInterval, m.gen)

	case tickMsg:
		if msg.gen != m.gen || m.loading {
			return m, nil
		}
		m.loading = true
		return m, tea.Batch(m.fetchCmd(), m.spinner.Tick)

	case spinner.TickMsg:
		if !m.loading {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) moveCursor(delta int) {
	if len(m.mrs) == 0 {
		return
	}
	if m.cursor < 0 {
		// Nothing selected yet: moving down starts at the top of what's on screen, moving up at
		// the last MR.
		if delta < 0 {
			m.cursor = len(m.mrs) - 1
		} else {
			m.cursor = m.offset
		}
	} else {
		m.cursor = max(0, min(len(m.mrs)-1, m.cursor+delta))
	}
	m.scroll()
}

func (m *model) applyMRs(mrs []MR) {
	next := make(map[string]string, len(mrs))
	m.changed = map[string]bool{}
	for _, mr := range mrs {
		sig := mr.Signature()
		next[mr.WebURL] = sig
		// Don't flag everything on the very first load.
		if m.prev != nil && m.prev[mr.WebURL] != sig {
			m.changed[mr.WebURL] = true
		}
	}
	m.prev = next
	m.bucket = mrs
	m.refilter()
}

// refilter rebuilds the shown rows from the bucket, keeping the selection on the same MR and
// dropping it if that MR is no longer shown.
func (m *model) refilter() {
	selected := ""
	if m.cursor >= 0 && m.cursor < len(m.mrs) {
		selected = m.mrs[m.cursor].WebURL
	}
	m.mrs = nil
	for _, mr := range m.bucket {
		if m.all || mr.Author.Username == m.me {
			m.mrs = append(m.mrs, mr)
		}
	}
	m.cursor = -1
	for i, mr := range m.mrs {
		if selected != "" && mr.WebURL == selected {
			m.cursor = i
			break
		}
	}
	m.scroll()
}

func (m model) showProject() bool { return len(m.cfg.Projects) != 1 }

// columns sizes the columns to the terminal width; title and labels share what's left.
func (m model) columns() []column {
	cols := []column{{" ", 1}, {"MR", 6}}
	if m.showProject() {
		cols = append(cols, column{"Project", 24})
	}
	if m.all {
		cols = append(cols, column{"Author", 16})
	}
	fixed := []column{{"Pipeline", 10}, {"Approvals", 9}, {"Threads", 7}, {"Status", 15}, {"Updated", 7}}

	used := 0
	for _, c := range append(append([]column{}, cols...), fixed...) {
		used += c.width + colGap
	}
	flex := max(40, m.width-used-2*colGap)
	// Labels get what the widest row's labels need on one line, up to 40%; the title gets the rest.
	labelsWidth := len("Labels")
	for _, mr := range m.mrs {
		w := 0
		for i, l := range mr.Labels.Nodes {
			if i > 0 {
				w++
			}
			w += lipgloss.Width(l.Title) + 2
		}
		labelsWidth = max(labelsWidth, w)
	}
	labelsWidth = min(labelsWidth, flex*2/5)
	titleWidth := flex - labelsWidth

	cols = append(cols, column{"Title", titleWidth}, column{"Labels", labelsWidth})
	return append(cols, fixed...)
}

// bodyHeight is the number of lines available for rows.
func (m model) bodyHeight() int {
	return max(3, m.height-5) // title, column header, header rule, status, help
}

// renderRow draws one MR. Rows are a single line; the selected row expands so the full title
// and every label are visible.
func (m model) renderRow(i int, cols []column) string {
	mr := m.mrs[i]
	selected := i == m.cursor
	titleCol := 0
	for j, c := range cols {
		if c.title == "Title" {
			titleCol = j
		}
	}
	title := mr.Title
	if !selected {
		title = ansi.Truncate(title, cols[titleCol].width, "…")
	}
	text := lipgloss.NewStyle()
	if selected {
		text = selectedStyle
	}

	mark := " "
	switch {
	case selected:
		mark = cursorStyle.Render("▌")
	case m.changed[mr.WebURL]:
		mark = changeStyle.Render("●")
	}

	cells := []string{mark, text.Render("!" + mr.IID)}
	if m.showProject() {
		cells = append(cells, text.Render(truncLeft(mr.Project.FullPath, 24)))
	}
	if m.all {
		author := ansi.Truncate(mr.Author.Username, 16, "…")
		if mr.Author.Username == m.me {
			cells = append(cells, cursorStyle.Bold(selected).Render(author))
		} else {
			cells = append(cells, text.Render(author))
		}
	}
	cells = append(cells,
		text.Render(title),
		labelsCell(mr.Labels.Nodes, cols[titleCol+1].width, selected),
		pipelineCell(mr.PipelineStatus()),
		text.Render(approvalsCell(mr)),
		text.Render(threadsCell(mr.UnresolvedThreads())),
		text.Render(statusCell(mr)),
		text.Render(ago(mr.UpdatedAt)),
	)

	parts := make([]string, len(cols))
	for j, c := range cols {
		parts[j] = lipgloss.NewStyle().Width(c.width + colGap).PaddingRight(colGap).Render(cells[j])
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// rowLines is a row as screen lines. An expanded selected row is framed by dividers so it stands
// apart from its single-line neighbours.
func (m model) rowLines(i int, cols []column) []string {
	lines := strings.Split(m.renderRow(i, cols), "\n")
	if i != m.cursor || len(lines) == 1 {
		return lines
	}
	divider := dimStyle.Render(strings.Repeat("┄", m.width))
	if i > m.offset {
		lines = append([]string{divider}, lines...)
	}
	if i < len(m.mrs)-1 {
		lines = append(lines, divider)
	}
	return lines
}

// scroll keeps the selected row fully on screen.
func (m *model) scroll() {
	if m.width == 0 || len(m.mrs) == 0 {
		m.offset = 0
		return
	}
	m.offset = min(m.offset, len(m.mrs)-1)
	if m.cursor < 0 {
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	cols := m.columns()
	avail := m.bodyHeight()
	for m.offset < m.cursor {
		sum := 0
		for i := m.offset; i <= m.cursor; i++ {
			sum += len(m.rowLines(i, cols))
		}
		if sum <= avail {
			break
		}
		m.offset++
	}
}

func (m model) View() string {
	if m.width == 0 {
		return ""
	}
	scope := "all projects"
	if len(m.cfg.Projects) > 0 {
		scope = strings.Join(m.cfg.Projects, ", ")
	}
	view := "My open MRs"
	if m.all {
		view = "All open MRs"
	}
	title := titleStyle.Render(fmt.Sprintf("%s (%d)", view, len(m.mrs))) + dimStyle.Render("  "+m.cfg.Host+" · "+scope)

	cols := m.columns()
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = headerStyle.Width(c.width + colGap).PaddingRight(colGap).Render(c.title)
	}
	rule := dimStyle.Render(strings.Repeat("─", m.width))

	avail := m.bodyHeight()
	var body []string
	for i := m.offset; i < len(m.mrs) && len(body) < avail; i++ {
		body = append(body, m.rowLines(i, cols)...)
	}
	if len(body) > avail {
		body = body[:avail]
	}
	for len(body) < avail {
		body = append(body, "")
	}

	var status string
	switch {
	case m.loading:
		status = m.spinner.View() + " refreshing…"
	case m.err != nil:
		status = errStyle.Render("error: " + m.err.Error())
	default:
		status = dimStyle.Render(fmt.Sprintf("updated %s · every %s", m.lastUpdate.Format("15:04:05"), m.cfg.RefreshInterval))
	}
	changed := 0
	for _, mr := range m.mrs {
		if m.changed[mr.WebURL] {
			changed++
		}
	}
	if changed > 0 && !m.loading {
		status += changeStyle.Render(fmt.Sprintf("  ● %d changed", changed))
	}
	if m.cursor >= 0 {
		status += dimStyle.Render(fmt.Sprintf("  · %d/%d", m.cursor+1, len(m.mrs)))
	}
	toggle := "a all MRs"
	if m.all {
		toggle = "a my MRs"
	}
	help := dimStyle.Render("↑/↓ select · enter/o open · esc deselect · " + toggle + " · r refresh · q quit")

	return strings.Join([]string{
		title,
		lipgloss.JoinHorizontal(lipgloss.Top, headers...),
		rule,
		strings.Join(body, "\n"),
		status,
		help,
	}, "\n")
}

// labelsCell renders labels as coloured pills. Wrapped, they take as many lines as needed;
// otherwise they stay on one line and the ones that don't fit are counted as "+N".
func labelsCell(labels []Label, width int, wrap bool) string {
	if len(labels) == 0 {
		return dimStyle.Render("-")
	}
	var lines []string
	line, lineWidth := "", 0
	for i, l := range labels {
		name := l.Title
		if r := []rune(name); len(r) > width-2 {
			name = string(r[:max(1, width-3)]) + "…"
		}
		pill := lipgloss.NewStyle().
			Background(lipgloss.Color(l.Color)).
			Foreground(lipgloss.Color(l.TextColor)).
			Render(" " + name + " ")
		w := lipgloss.Width(pill)
		if !wrap {
			more := ""
			if rest := len(labels) - i - 1; rest > 0 {
				more = fmt.Sprintf(" +%d", rest)
			}
			if lineWidth > 0 && lineWidth+1+w+len(more) > width {
				return line + dimStyle.Render(fmt.Sprintf(" +%d", len(labels)-i))
			}
		}
		if lineWidth > 0 && lineWidth+1+w > width {
			lines = append(lines, line)
			line, lineWidth = "", 0
		}
		if lineWidth > 0 {
			line += " "
			lineWidth++
		}
		line += pill
		lineWidth += w
	}
	return strings.Join(append(lines, line), "\n")
}

func pipelineCell(s string) string {
	var text string
	switch s {
	case "":
		return dimStyle.Render("-")
	case "SUCCESS":
		text = "✔ passed"
	case "FAILED":
		text = "✘ failed"
	case "RUNNING":
		text = "▶ running"
	case "PENDING", "CREATED", "WAITING_FOR_RESOURCE", "PREPARING", "SCHEDULED", "WAITING_FOR_CALLBACK":
		text = "… pending"
	case "CANCELED", "CANCELING":
		text = "⊘ canceled"
	case "SKIPPED":
		text = "» skipped"
	case "MANUAL":
		text = "⚙ manual"
	default:
		text = strings.ToLower(s)
	}
	if c, ok := pipelineColors[s]; ok {
		return lipgloss.NewStyle().Foreground(c).Render(text)
	}
	return text
}

func approvalsCell(mr MR) string {
	if mr.Approved && mr.ApprovalsLeft == 0 {
		return "✔"
	}
	if mr.ApprovalsRequired == 0 {
		return "-"
	}
	return fmt.Sprintf("%d/%d", mr.ApprovalsRequired-mr.ApprovalsLeft, mr.ApprovalsRequired)
}

func threadsCell(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("%d open", n)
}

func statusCell(mr MR) string {
	if mr.MergeTrainIndex != nil {
		return fmt.Sprintf("in train #%d", *mr.MergeTrainIndex+1)
	}
	if mr.Conflicts {
		return "conflicts"
	}
	if mr.AutoMergeEnabled {
		switch mr.AutoMergeStrategy {
		case "add_to_merge_train_when_checks_pass":
			return "train after CI"
		case "merge_train":
			return "in train"
		default:
			return "auto-merge"
		}
	}
	switch mr.DetailedMergeStatus {
	case "MERGEABLE":
		return "ready to merge"
	case "DRAFT_STATUS":
		return "draft"
	case "UNCHECKED", "CHECKING", "APPROVALS_SYNCING":
		if mr.Draft {
			return "draft"
		}
		return "checking"
	case "CI_MUST_PASS", "CI_STILL_RUNNING":
		return "waiting for CI"
	case "NOT_APPROVED":
		return "needs approval"
	case "DISCUSSIONS_NOT_RESOLVED":
		return "open threads"
	case "NEED_REBASE":
		return "needs rebase"
	case "BLOCKED_STATUS":
		return "blocked"
	}
	return strings.ToLower(strings.ReplaceAll(mr.DetailedMergeStatus, "_", " "))
}

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func truncLeft(s string, w int) string {
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	return "…" + string(r[len(r)-w+1:])
}

func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, url).Start()
}
