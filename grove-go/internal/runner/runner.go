// Package runner implements the bubbletea TUI for `gw run`.
// It manages multiple dev processes side-by-side: a sidebar lists repos with
// status indicators, and a log pane streams stdout+stderr for the selected
// process.
package runner

import (
	"bufio"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Gruvbox palette ──────────────────────────────────────────────────────────

var (
	colorGreen  = lipgloss.Color("#b8bb26")
	colorYellow = lipgloss.Color("#fabd2f")
	colorRed    = lipgloss.Color("#fb4934")
	colorDim    = lipgloss.Color("#928374")
	colorFg1    = lipgloss.Color("#ebdbb2")
	colorFg4    = lipgloss.Color("#a89984")
	colorBg1    = lipgloss.Color("#3c3836")
	colorBg2    = lipgloss.Color("#504945")
)

// ── Styles ───────────────────────────────────────────────────────────────────

var (
	styleHeader = lipgloss.NewStyle().
			Background(colorBg1).
			Foreground(colorYellow).
			Bold(true).
			Padding(0, 1)

	styleHeaderSpacer = lipgloss.NewStyle().
				Background(colorBg1).
				Foreground(colorFg4)

	styleHeaderRight = lipgloss.NewStyle().
				Background(colorBg1).
				Foreground(colorFg4).
				Padding(0, 1)

	styleFooter = lipgloss.NewStyle().
			Background(colorBg1).
			Foreground(colorFg4).
			Padding(0, 1)

	// Panel borders (rounded) — dim when unselected.
	stylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBg2)

	stylePanelTitle = lipgloss.NewStyle().
			Foreground(colorFg4).
			Bold(true)

	styleLogTitle = lipgloss.NewStyle().
			Foreground(colorFg1).
			Bold(true)

	styleSelected = lipgloss.NewStyle().
			Background(colorBg2).
			Foreground(colorFg1).
			Bold(true)

	styleNormal = lipgloss.NewStyle().
			Foreground(colorFg1)

	styleLogLine = lipgloss.NewStyle().
			Foreground(colorFg1)

	styleLogDim = lipgloss.NewStyle().
			Foreground(colorDim).
			Italic(true)

	// Status dots rendered once and reused.
	dotGreen  = lipgloss.NewStyle().Foreground(colorGreen).Render("●")
	dotYellow = lipgloss.NewStyle().Foreground(colorYellow).Render("●")
	dotRed    = lipgloss.NewStyle().Foreground(colorRed).Render("●")
	dotDimStr = lipgloss.NewStyle().Foreground(colorDim).Render("●")
)

// ── Public types ─────────────────────────────────────────────────────────────

// Entry describes a single process managed by the runner TUI.
type Entry struct {
	RepoName string
	Command  string
	Cwd      string
}

// ── Internal process state ───────────────────────────────────────────────────

type procStatus int

const (
	statusStarting procStatus = iota
	statusRunning
	statusExitedOK
	statusExitedErr
)

const maxLogLines = 1000

// processState tracks one subprocess and its log buffer.
type processState struct {
	entry    Entry
	status   procStatus
	exitCode int
	logs     []string

	// live subprocess handles — set after Start()
	cmd     *exec.Cmd
	scanner *bufio.Scanner
}

func (ps *processState) dot() string {
	switch ps.status {
	case statusStarting:
		return dotYellow
	case statusRunning:
		return dotGreen
	case statusExitedOK:
		return dotDimStr
	case statusExitedErr:
		return dotRed
	default:
		return dotDimStr
	}
}

func (ps *processState) appendLog(line string) {
	ps.logs = append(ps.logs, line)
	if len(ps.logs) > maxLogLines {
		// Amortise by dropping the oldest quarter in one slice op.
		drop := maxLogLines / 4
		ps.logs = ps.logs[drop:]
	}
}

// ── Messages ─────────────────────────────────────────────────────────────────

type procStartedMsg struct{ idx int }
type logLineMsg struct {
	idx  int
	line string
}
type procExitedMsg struct {
	idx      int
	exitCode int
}

// ── Commands ─────────────────────────────────────────────────────────────────

// startCmd launches the subprocess for ps and returns procStartedMsg on success
// or procExitedMsg(-1) on failure. It attaches a bufio.Scanner to the merged
// stdout/stderr pipe so that readNextCmd can read lines incrementally.
func startCmd(ps *processState, idx int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("sh", "-c", ps.entry.Command) //nolint:gosec
		cmd.Dir = ps.entry.Cwd
		// New process group so we can signal the whole tree on quit/restart.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return procExitedMsg{idx: idx, exitCode: -1}
		}
		cmd.Stderr = cmd.Stdout // merge stderr into stdout pipe

		if err := cmd.Start(); err != nil {
			return procExitedMsg{idx: idx, exitCode: -1}
		}

		ps.cmd = cmd
		ps.scanner = bufio.NewScanner(pipe)
		return procStartedMsg{idx: idx}
	}
}

// readNextCmd reads the next line from ps.scanner. If a line is available it
// returns logLineMsg and must be called again. When the scanner is exhausted it
// waits for the process to finish and returns procExitedMsg.
func readNextCmd(ps *processState, idx int) tea.Cmd {
	return func() tea.Msg {
		if ps.scanner == nil {
			return procExitedMsg{idx: idx, exitCode: -1}
		}
		if ps.scanner.Scan() {
			return logLineMsg{idx: idx, line: ps.scanner.Text()}
		}
		// EOF — reap the process.
		exitCode := 0
		if err := ps.cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		return procExitedMsg{idx: idx, exitCode: exitCode}
	}
}

// killProcess sends SIGTERM to the process group, waits up to 5 s, then
// SIGKILLs. It is safe to call even if the process has already exited.
func killProcess(ps *processState) {
	if ps.cmd == nil || ps.cmd.Process == nil {
		return
	}
	pgid, pgErr := syscall.Getpgid(ps.cmd.Process.Pid)
	if pgErr == nil {
		_ = syscall.Kill(-pgid, syscall.SIGTERM)
	} else {
		_ = ps.cmd.Process.Signal(syscall.SIGTERM)
	}

	done := make(chan struct{})
	go func() {
		// Wait may already have been called by readNextCmd; ignore the error.
		_ = ps.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		if pgErr == nil {
			_ = syscall.Kill(-pgid, syscall.SIGKILL)
		} else {
			_ = ps.cmd.Process.Kill()
		}
		<-done
	}
}

// ── Model ────────────────────────────────────────────────────────────────────

const sidebarWidth = 32

type model struct {
	width    int
	height   int
	procs    []*processState
	selected int
}

func newModel(entries []Entry) model {
	procs := make([]*processState, len(entries))
	for i, e := range entries {
		procs[i] = &processState{entry: e, status: statusStarting}
	}
	return model{procs: procs}
}

// ── Init ─────────────────────────────────────────────────────────────────────

func (m model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.procs))
	for i, ps := range m.procs {
		cmds[i] = startCmd(ps, i)
	}
	return tea.Batch(cmds...)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case procStartedMsg:
		ps := m.procs[msg.idx]
		ps.status = statusRunning
		// Begin streaming lines.
		return m, readNextCmd(ps, msg.idx)

	case logLineMsg:
		ps := m.procs[msg.idx]
		ps.appendLog(msg.line)
		// Fetch the next line.
		return m, readNextCmd(ps, msg.idx)

	case procExitedMsg:
		ps := m.procs[msg.idx]
		ps.exitCode = msg.exitCode
		if msg.exitCode == 0 {
			ps.status = statusExitedOK
		} else {
			ps.status = statusExitedErr
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	n := len(m.procs)

	switch msg.String() {
	case "q", "ctrl+c":
		// Kill all processes then quit.
		return m, tea.Sequence(
			func() tea.Msg {
				for _, ps := range m.procs {
					killProcess(ps)
				}
				return nil
			},
			tea.Quit,
		)

	case "r":
		if n == 0 {
			break
		}
		ps := m.procs[m.selected]
		killProcess(ps)
		ps.logs = ps.logs[:0]
		ps.status = statusStarting
		ps.cmd = nil
		ps.scanner = nil
		return m, startCmd(ps, m.selected)

	case "j", "down":
		if m.selected < n-1 {
			m.selected++
		}

	case "k", "up":
		if m.selected > 0 {
			m.selected--
		}

	case "g":
		m.selected = 0

	case "G":
		if n > 0 {
			m.selected = n - 1
		}

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0]-'0') - 1
		if idx >= 0 && idx < n {
			m.selected = idx
		}
	}

	return m, nil
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m model) View() string {
	if m.width == 0 {
		return ""
	}

	header := m.viewHeader()
	footer := m.viewFooter()
	bodyH := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyH < 1 {
		bodyH = 1
	}

	sidebar := m.viewSidebar(sidebarWidth, bodyH)
	logPane := m.viewLogPane(m.width-sidebarWidth, bodyH)

	body := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, logPane)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

func (m model) viewHeader() string {
	title := styleHeader.Render("grove run")
	info := styleHeaderRight.Render(m.headerInfo())
	gap := styleHeaderSpacer.
		Background(colorBg1).
		Width(m.width - lipgloss.Width(title) - lipgloss.Width(info)).
		Render("")
	return lipgloss.JoinHorizontal(lipgloss.Top, title, gap, info)
}

func (m model) headerInfo() string {
	running, failed, exited := 0, 0, 0
	for _, ps := range m.procs {
		switch ps.status {
		case statusRunning:
			running++
		case statusExitedOK:
			exited++
		case statusExitedErr:
			failed++
		}
	}
	var parts []string
	if running > 0 {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorGreen).Render(fmt.Sprintf("%d running", running)))
	}
	if failed > 0 {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorRed).Render(fmt.Sprintf("%d failed", failed)))
	}
	if exited > 0 {
		parts = append(parts,
			lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%d exited", exited)))
	}
	if len(parts) == 0 {
		return lipgloss.NewStyle().Foreground(colorYellow).Render("starting…")
	}
	return strings.Join(parts, "  ")
}

func (m model) viewFooter() string {
	return styleFooter.Width(m.width).Render("j/k navigate  r restart  1-9 jump  q quit")
}

func (m model) viewSidebar(width, height int) string {
	// The panel border consumes 1 char on each side.
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}

	title := stylePanelTitle.Render(fmt.Sprintf("Repos (%d)", len(m.procs)))
	rows := []string{title}

	// How many repo rows fit: total height minus border(2) minus title(1) minus blank(1).
	visibleRows := height - 4
	if visibleRows < 1 {
		visibleRows = 1
	}

	// Scroll so the selected row stays visible.
	offset := 0
	if m.selected >= visibleRows {
		offset = m.selected - visibleRows + 1
	}

	for i := offset; i < len(m.procs) && i-offset < visibleRows; i++ {
		ps := m.procs[i]
		label := ps.dot() + " " + ps.entry.RepoName
		if i == m.selected {
			rows = append(rows, styleSelected.Width(innerW).Render(label))
		} else {
			rows = append(rows, styleNormal.Render(label))
		}
	}

	return stylePanel.
		Width(innerW).
		Height(height - 2).
		Render(strings.Join(rows, "\n"))
}

func (m model) viewLogPane(width, height int) string {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}

	var titleStr string
	var logs []string

	if len(m.procs) == 0 {
		titleStr = styleLogDim.Render("no processes")
	} else {
		ps := m.procs[m.selected]
		titleStr = styleLogTitle.Render(ps.entry.RepoName + " — " + ps.entry.Command)
		logs = ps.logs
	}

	// Visible log rows: height minus border(2) minus title(1) minus blank separator(1).
	visibleLines := height - 4
	if visibleLines < 1 {
		visibleLines = 1
	}

	// Always tail the log buffer.
	start := 0
	if len(logs) > visibleLines {
		start = len(logs) - visibleLines
	}
	visible := logs[start:]

	rows := make([]string, 0, 2+len(visible))
	rows = append(rows, titleStr, "")
	for _, line := range visible {
		if lipgloss.Width(line) > innerW {
			line = truncateAnsi(line, innerW)
		}
		rows = append(rows, styleLogLine.Render(line))
	}

	return stylePanel.
		Width(innerW).
		Height(height - 2).
		Render(strings.Join(rows, "\n"))
}

// ── Helpers ──────────────────────────────────────────────────────────────────

// truncateAnsi truncates s so it occupies at most n visible columns, appending
// "…" if truncation occurs. This is a best-effort approach for plain text; for
// ANSI-escaped output the visible width may differ.
func truncateAnsi(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(runes[:n-1]) + "…"
}

// ── Public entry point ───────────────────────────────────────────────────────

// Run launches the runner TUI. It blocks until the user quits.
// Each Entry's Command is run via "sh -c" with its Cwd as the working
// directory; stdout and stderr are merged and streamed into the log panel.
func Run(entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	p := tea.NewProgram(
		newModel(entries),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
