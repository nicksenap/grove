package dash

import (
	"fmt"
	"os"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nicksenap/grove/internal/models"
	"github.com/nicksenap/grove/internal/state"
	"github.com/nicksenap/grove/internal/workspace"
)

// Version is injected at build time; dash imports it via Dash().
var appVersion = "dev"

// focus tracks which panel has keyboard focus.
type focus int

const (
	focusList focus = iota
	focusDetail
)

// panel identifies a UI panel for consistent border rendering.
type panel int

const (
	panelList   panel = iota
	panelDetail panel = iota
)

// --- Messages ---

// workspacesLoadedMsg carries the freshly-read workspace list.
type workspacesLoadedMsg struct {
	workspaces []models.Workspace
	err        error
}

// statusLoadedMsg carries git status results for a single workspace.
type statusLoadedMsg struct {
	workspaceName string
	entries       []workspace.StatusEntry
	err           error
}

// --- Model ---

// Model is the root bubbletea model for the Grove dashboard.
type Model struct {
	// Layout
	width  int
	height int

	// Focus
	focus focus

	// Data
	workspaces []models.Workspace
	loadErr    error

	// List state
	cursor int

	// Status cache: workspace name → fetched entries
	statusCache    map[string][]workspace.StatusEntry
	statusErrCache map[string]error
	loadingStatus  map[string]bool

	// View mode
	showHelp bool

	// Version string passed from CLI
	version string
}

// newModel returns a zeroed model ready for Init.
func newModel(version string) Model {
	return Model{
		version:        version,
		statusCache:    make(map[string][]workspace.StatusEntry),
		statusErrCache: make(map[string]error),
		loadingStatus:  make(map[string]bool),
		focus:          focusList,
	}
}

// selectedWorkspace returns a pointer to the currently highlighted workspace,
// or nil when the list is empty.
func (m *Model) selectedWorkspace() *models.Workspace {
	if len(m.workspaces) == 0 || m.cursor < 0 || m.cursor >= len(m.workspaces) {
		return nil
	}
	return &m.workspaces[m.cursor]
}

// --- Commands ---

// loadWorkspacesCmd reads state.json asynchronously.
func loadWorkspacesCmd() tea.Cmd {
	return func() tea.Msg {
		wss, err := state.LoadWorkspaces()
		return workspacesLoadedMsg{workspaces: wss, err: err}
	}
}

// fetchStatusCmd calls workspace.Status in a goroutine and returns results.
func fetchStatusCmd(ws models.Workspace) tea.Cmd {
	return func() tea.Msg {
		entries := workspace.Status(&ws)
		return statusLoadedMsg{
			workspaceName: ws.Name,
			entries:       entries,
		}
	}
}

// --- Init ---

// Init satisfies tea.Model; kicks off the initial workspace load.
func (m Model) Init() tea.Cmd {
	return loadWorkspacesCmd()
}

// --- Update ---

// Update satisfies tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case workspacesLoadedMsg:
		if msg.err != nil {
			m.loadErr = msg.err
			return m, nil
		}
		m.workspaces = msg.workspaces
		m.loadErr = nil
		// Clamp cursor
		if m.cursor >= len(m.workspaces) {
			m.cursor = max(0, len(m.workspaces)-1)
		}
		// Kick off status fetch for the selected workspace.
		return m, m.maybeFetchStatus()

	case statusLoadedMsg:
		delete(m.loadingStatus, msg.workspaceName)
		if msg.err != nil {
			m.statusErrCache[msg.workspaceName] = msg.err
		} else {
			m.statusCache[msg.workspaceName] = msg.entries
		}

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey processes keyboard input.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.showHelp {
		// Any key dismisses help
		m.showHelp = false
		return m, nil
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "?":
		m.showHelp = true

	case "tab":
		if m.focus == focusList {
			m.focus = focusDetail
		} else {
			m.focus = focusList
		}

	// Navigation — always drive the list regardless of which panel is focused
	case "j", "down":
		if m.cursor < len(m.workspaces)-1 {
			m.cursor++
			return m, m.maybeFetchStatus()
		}

	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			return m, m.maybeFetchStatus()
		}

	case "g":
		m.cursor = 0
		return m, m.maybeFetchStatus()

	case "G":
		m.cursor = max(0, len(m.workspaces)-1)
		return m, m.maybeFetchStatus()

	case "r", "ctrl+r":
		// Refresh workspace list and re-fetch status for selected workspace.
		return m, tea.Batch(loadWorkspacesCmd(), m.maybeFetchStatus())

	case "enter":
		// Move focus to detail panel if not already there.
		m.focus = focusDetail
	}

	return m, nil
}

// maybeFetchStatus returns a command to fetch status for the selected workspace
// if it hasn't been fetched or is not already in flight.
func (m *Model) maybeFetchStatus() tea.Cmd {
	ws := m.selectedWorkspace()
	if ws == nil {
		return nil
	}
	if m.loadingStatus[ws.Name] {
		return nil
	}
	if _, cached := m.statusCache[ws.Name]; cached {
		return nil
	}
	m.loadingStatus[ws.Name] = true
	return fetchStatusCmd(*ws)
}

// --- View ---

// View satisfies tea.Model.
func (m Model) View() string {
	if m.width == 0 {
		return "" // Not yet sized
	}

	if m.showHelp {
		return m.viewHelp()
	}

	header := m.viewHeader()
	footer := m.viewFooter()

	// Reserve header + footer rows (each is 1 line)
	bodyHeight := m.height - lipgloss.Height(header) - lipgloss.Height(footer)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Split body: 40% list, 60% detail (minimum 24 chars for list panel)
	listWidth := m.width * 40 / 100
	if listWidth < 24 {
		listWidth = 24
	}
	if listWidth > m.width-20 {
		listWidth = m.width - 20
	}
	detailWidth := m.width - listWidth

	listView := m.viewList(listWidth, bodyHeight)
	detailView := m.viewDetail(detailWidth, bodyHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, listView, detailView)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, footer)
}

// viewHeader renders the top bar.
func (m Model) viewHeader() string {
	title := styleHeader.Render("grove")
	ver := styleHeaderDim.
		Background(colorBg1).
		Render(fmt.Sprintf(" v%s", m.version))
	space := styleHeaderDim.
		Background(colorBg1).
		Width(m.width - lipgloss.Width(title) - lipgloss.Width(ver)).
		Render("")
	return lipgloss.JoinHorizontal(lipgloss.Top, title, space, ver)
}

// viewFooter renders the bottom keybinding bar.
func (m Model) viewFooter() string {
	hints := "j/k navigate  tab focus  r refresh  ? help  q quit"
	return styleFooter.Width(m.width).Render(hints)
}

// viewList renders the left workspace list panel.
func (m Model) viewList(width, height int) string {
	// Inner content width accounts for border (1 char each side)
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}

	title := stylePanelTitle.Render("Workspaces")
	// Header row inside panel
	var rows []string
	rows = append(rows, title)

	if m.loadErr != nil {
		rows = append(rows, styleMissing.Width(innerW).Render(fmt.Sprintf("error: %v", m.loadErr)))
	} else if len(m.workspaces) == 0 {
		rows = append(rows, styleNormalDim.Width(innerW).Render("no workspaces — run 'gw new'"))
	} else {
		// Visible window: leave 1 row for the title + 1 for padding
		visibleRows := height - 4
		if visibleRows < 1 {
			visibleRows = 1
		}

		// Scroll so cursor is always visible
		offset := 0
		if m.cursor >= visibleRows {
			offset = m.cursor - visibleRows + 1
		}

		for i := offset; i < len(m.workspaces) && i-offset < visibleRows; i++ {
			ws := m.workspaces[i]
			selected := i == m.cursor
			row := m.renderWorkspaceRow(ws, selected, innerW)
			rows = append(rows, row)
		}
	}

	content := strings.Join(rows, "\n")

	border := stylePanel
	if m.focus == focusList {
		border = stylePanelFocused
	}

	return border.
		Width(innerW).
		Height(height - 2).
		Render(content)
}

// renderWorkspaceRow renders a single row in the workspace list.
func (m Model) renderWorkspaceRow(ws models.Workspace, selected bool, width int) string {
	// Indicator glyph
	indicator := "  "
	if selected {
		indicator = "> "
	}

	// Name segment
	namePart := ws.Name
	if selected {
		namePart = styleSelected.Render(indicator + namePart)
	} else {
		namePart = styleNormal.Render(indicator + namePart)
	}

	// Branch segment
	branchPart := styleBranchLabel.Render(" " + ws.Branch)

	// Repo count
	repoCountPart := styleNormalDim.Render(fmt.Sprintf(" [%d]", len(ws.Repos)))

	row := namePart + branchPart + repoCountPart

	// Truncate if too wide
	rowWidth := lipgloss.Width(row)
	if rowWidth > width {
		// Trim name only; keep branch visible
		maxName := width - lipgloss.Width(branchPart) - lipgloss.Width(repoCountPart) - 2
		if maxName < 4 {
			maxName = 4
		}
		truncated := ws.Name
		if len(truncated) > maxName {
			truncated = truncated[:maxName-1] + "…"
		}
		if selected {
			namePart = styleSelected.Render(indicator + truncated)
		} else {
			namePart = styleNormal.Render(indicator + truncated)
		}
		row = namePart + branchPart + repoCountPart
	}

	return row
}

// viewDetail renders the right detail panel for the selected workspace.
func (m Model) viewDetail(width, height int) string {
	innerW := width - 2
	if innerW < 1 {
		innerW = 1
	}

	var rows []string

	ws := m.selectedWorkspace()
	if ws == nil {
		rows = append(rows, styleNormalDim.Render("select a workspace"))
	} else {
		rows = append(rows, stylePanelTitle.Render(ws.Name))
		rows = append(rows, "")

		// Workspace metadata
		rows = append(rows, m.kv("branch", styleBranchLabel.Render(ws.Branch)))
		rows = append(rows, m.kv("path", styleNormalDim.Render(shortenPath(ws.Path))))
		rows = append(rows, m.kv("repos", styleNormal.Render(fmt.Sprintf("%d", len(ws.Repos)))))
		rows = append(rows, m.kv("created", styleNormalDim.Render(shortDate(ws.CreatedAt))))
		rows = append(rows, "")

		// Check if workspace dir exists
		if _, err := os.Stat(ws.Path); os.IsNotExist(err) {
			rows = append(rows, styleMissing.Render("directory missing"))
			rows = append(rows, "")
		}

		// Repos section
		rows = append(rows, stylePanelTitle.Render("Repos"))
		rows = append(rows, "")

		if m.loadingStatus[ws.Name] {
			rows = append(rows, styleLoading.Render("fetching status..."))
		} else if errStr, hasErr := m.statusErrCache[ws.Name]; hasErr {
			rows = append(rows, styleDirty.Render(fmt.Sprintf("status error: %v", errStr)))
		} else if entries, ok := m.statusCache[ws.Name]; ok {
			// Build a lookup by repo name for fast access
			entryMap := make(map[string]workspace.StatusEntry, len(entries))
			for _, e := range entries {
				entryMap[e.Repo] = e
			}
			for _, repo := range ws.Repos {
				rows = append(rows, m.renderRepoDetail(repo, entryMap[repo.RepoName], innerW))
			}
		} else {
			// Status not fetched yet — show repo names without status
			for _, repo := range ws.Repos {
				rows = append(rows, styleRepoName.Render("  "+repo.RepoName))
			}
		}
	}

	content := strings.Join(rows, "\n")

	border := stylePanel
	if m.focus == focusDetail {
		border = stylePanelFocused
	}

	return border.
		Width(innerW).
		Height(height - 2).
		Render(content)
}

// renderRepoDetail renders a single repo's status inside the detail panel.
func (m Model) renderRepoDetail(repo models.RepoWorktree, entry workspace.StatusEntry, width int) string {
	name := styleRepoName.Render("  " + repo.RepoName)

	// Status badge
	statusText := ""
	if entry.Repo == "" {
		// Zero value — no entry yet
		statusText = styleNormalDim.Render(" (pending)")
	} else {
		trimmed := strings.TrimSpace(entry.Status)
		if trimmed == "" {
			statusText = "  " + styleClean.Render("clean")
		} else {
			lines := strings.Count(trimmed, "\n") + 1
			statusText = "  " + styleDirty.Render(fmt.Sprintf("%d changes", lines))
		}

		// Ahead / behind
		if entry.Ahead > 0 {
			statusText += " " + styleAhead.Render(fmt.Sprintf("↑%d", entry.Ahead))
		}
		if entry.Behind > 0 {
			statusText += " " + styleBehind.Render(fmt.Sprintf("↓%d", entry.Behind))
		}
	}

	return name + statusText
}

// viewHelp renders the full-screen help overlay.
func (m Model) viewHelp() string {
	type binding struct {
		key  string
		desc string
	}
	bindings := []binding{
		{"j / ↓", "move down"},
		{"k / ↑", "move up"},
		{"g", "go to top"},
		{"G", "go to bottom"},
		{"enter", "focus detail panel"},
		{"tab", "toggle panel focus"},
		{"r / ctrl+r", "refresh workspaces + status"},
		{"?", "toggle this help"},
		{"q / ctrl+c", "quit"},
	}

	var sb strings.Builder
	sb.WriteString(stylePanelTitle.Render("Keyboard shortcuts") + "\n\n")
	for _, b := range bindings {
		key := styleHelpKey.Width(16).Render(b.key)
		desc := styleHelpDesc.Render(b.desc)
		sb.WriteString(key + "  " + desc + "\n")
	}
	sb.WriteString("\n" + styleNormalDim.Render("press any key to close"))

	inner := sb.String()
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorYellow).
		Padding(1, 3).
		Render(inner)

	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// kv returns a formatted key: value line for the detail panel.
func (m Model) kv(key, val string) string {
	return styleStatusKey.Render(key+":") + " " + val
}

// --- Helpers ---

// shortenPath replaces the home directory prefix with "~".
func shortenPath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, home) {
		return "~" + path[len(home):]
	}
	return path
}

// shortDate trims RFC3339 to date only (first 10 chars).
func shortDate(ts string) string {
	if len(ts) >= 10 {
		return ts[:10]
	}
	return ts
}

// max returns the larger of a and b (Go 1.21+ has builtin max, but kept for clarity).
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- Concurrent status prefetch ---

// prefetchAllStatuses fetches status for every workspace concurrently and
// populates the model's cache before the event loop starts. It is called
// only in non-interactive (batch/test) scenarios; the TUI uses per-selection
// lazy fetching instead.
func prefetchAllStatuses(m *Model) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, ws := range m.workspaces {
		wg.Add(1)
		go func(ws models.Workspace) {
			defer wg.Done()
			entries := workspace.Status(&ws)
			mu.Lock()
			m.statusCache[ws.Name] = entries
			mu.Unlock()
		}(ws)
	}
	wg.Wait()
}

// --- Entry point ---

// Dash launches the Grove TUI dashboard. It is called from the CLI.
func Dash(version string) error {
	m := newModel(version)

	p := tea.NewProgram(
		m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
