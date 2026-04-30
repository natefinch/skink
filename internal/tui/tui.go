// Package tui holds the Bubble Tea UI models used by skink. Each exported
// Run* function drives one interactive screen and returns the user's choice
// or an error. Higher layers (cli) call these; lower layers (config,
// installer, etc.) know nothing about this package.
package tui

import (
	"errors"
	"fmt"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ErrCancelled is returned when the user cancels the active UI.
var ErrCancelled = errors.New("tui: cancelled by user")

// ErrBack is returned when the user backs out of the active UI.
var ErrBack = errors.New("tui: back requested")

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE6FF8"))
	selectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

const cursorMarker = "🦎 "

// statusLogoLines is the multi-line "SKINK" banner. Each line is rendered with
// its own color in renderStatusLogo to produce a vertical gradient that fades
// from pink at the top to light blue at the bottom.
var statusLogoLines = []string{
	`  ██████╗ ██╗  ██╗██╗███╗   ██╗██╗  ██╗`,
	`  ██╔════╝██║ ██╔╝██║████╗  ██║██║ ██╔╝`,
	`  ███████╗█████╔╝ ██║██╔██╗ ██║█████╔╝ `,
	`  ╚════██║██╔═██╗ ██║██║╚██╗██║██╔═██╗ `,
	`  ███████║██║  ██╗██║██║ ╚████║██║  ██╗`,
	`  ╚══════╝╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝`,
}

// statusLogoColors holds the per-row colors of the SKINK banner: hot pink at
// the top fading down to light blue at the bottom.
var statusLogoColors = []string{
	"#EF7FBE",
	"#DE95C8",
	"#CEABD2",
	"#BDC1DC",
	"#ADD8E6",
	"#ADD8E6",
}

func renderStatusLogo() string {
	var b strings.Builder
	for i, line := range statusLogoLines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(statusLogoColors[i])).Render(line))
	}
	return b.String()
}

func runModel[T tea.Model](m T) (T, error) {
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return m, err
	}
	res, ok := final.(T)
	if !ok {
		return m, fmt.Errorf("tui: unexpected final model %T", final)
	}
	return res, nil
}

func newView(s string) tea.View {
	return tea.NewView(s)
}

func newStatusView(s string) tea.View {
	v := newView(s)
	v.AltScreen = true
	return v
}

func selectedIndices[T any](items []T, selected map[int]bool) []int {
	out := make([]int, 0, len(selected))
	for i := range items {
		if selected[i] {
			out = append(out, i)
		}
	}
	return out
}

// RunTextPrompt shows a single-line text input with the given prompt and
// returns the entered (trimmed) value. An empty value is rejected.
func RunTextPrompt(title, prompt, placeholder string) (string, error) {
	m := newTextModel(title, prompt, placeholder)
	res, err := runModel(m)
	if err != nil {
		return "", err
	}
	if res.cancelled {
		return "", ErrCancelled
	}
	return strings.TrimSpace(res.value), nil
}

func newTextModel(title, prompt, placeholder string) textModel {
	ti := textinput.New()
	ti.Placeholder = placeholder
	_ = ti.Focus()
	ti.CharLimit = 512
	ti.SetWidth(64)

	return textModel{title: title, prompt: prompt, input: ti}
}

type textModel struct {
	title     string
	prompt    string
	input     textinput.Model
	value     string
	err       string
	cancelled bool
	done      bool
}

func (m textModel) Init() tea.Cmd { return textinput.Blink }

func (m textModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "enter":
			v := strings.TrimSpace(m.input.Value())
			if v == "" {
				m.err = "value cannot be empty"
				return m, nil
			}
			m.value = v
			m.done = true
			return m, tea.Quit
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m textModel) View() tea.View {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}
	b.WriteString(m.prompt)
	b.WriteString("\n")
	b.WriteString(m.input.View())
	b.WriteString("\n")
	if m.err != "" {
		b.WriteString(errStyle.Render(m.err))
		b.WriteString("\n")
	}
	b.WriteString(helpStyle.Render("enter to confirm • esc to cancel"))
	return newView(b.String())
}

// RunMultiSelect shows a list of items with checkboxes. Returns the indices
// selected (sorted). An empty selection is rejected unless allowEmpty is
// true.
func RunMultiSelect(title string, items []string, allowEmpty bool) ([]int, error) {
	if len(items) == 0 {
		return nil, errors.New("tui: no items to select from")
	}
	m := multiModel{title: title, items: items, allowEmpty: allowEmpty, selected: make(map[int]bool)}
	res, err := runModel(m)
	if err != nil {
		return nil, err
	}
	if res.cancelled {
		return nil, ErrCancelled
	}
	return selectedIndices(res.items, res.selected), nil
}

type multiModel struct {
	title      string
	items      []string
	selected   map[int]bool
	cursor     int
	allowEmpty bool
	cancelled  bool
	err        string
}

func (m multiModel) Init() tea.Cmd { return nil }

func (m multiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "space":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			all := true
			for i := range m.items {
				if !m.selected[i] {
					all = false
					break
				}
			}
			for i := range m.items {
				m.selected[i] = !all
			}
		case "enter":
			if !m.allowEmpty && len(m.chosen()) == 0 {
				m.err = "select at least one (space to toggle)"
				return m, nil
			}
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m multiModel) chosen() []int {
	out := make([]int, 0, len(m.selected))
	for i, v := range m.selected {
		if v {
			out = append(out, i)
		}
	}
	return out
}

func (m multiModel) View() tea.View {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}
	for i, it := range m.items {
		cursor := listCursor(i == m.cursor)
		check := "[ ]"
		if m.selected[i] {
			check = selectStyle.Render("[x]")
		}
		fmt.Fprintf(&b, "%s%s %s\n", cursor, check, it)
	}
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("space toggle • a all/none • enter confirm • esc cancel"))
	return newView(b.String())
}

// RunSingleSelect shows a list and returns the chosen index.
func RunSingleSelect(title string, items []string) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("tui: no items to select from")
	}
	m := singleModel{title: title, items: items}
	res, err := runModel(m)
	if err != nil {
		return 0, err
	}
	if res.cancelled {
		return 0, ErrCancelled
	}
	return res.cursor, nil
}

type singleModel struct {
	title     string
	items     []string
	cursor    int
	cancelled bool
}

func (m singleModel) Init() tea.Cmd { return nil }
func (m singleModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			return m, tea.Quit
		}
	}
	return m, nil
}
func (m singleModel) View() tea.View {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}
	for i, it := range m.items {
		cursor := listCursor(i == m.cursor)
		fmt.Fprintf(&b, "%s%s\n", cursor, it)
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ move • enter select • esc cancel"))
	return newView(b.String())
}

// BrowseItem is one skill row shown by RunBrowseSelect.
type BrowseItem struct {
	Name        string
	Path        string
	Description string
	Selected    bool
	SelectsAll  bool
}

// RunBrowseSelect shows an expandable multi-select list of discovered skills.
func RunBrowseSelect(title string, items []BrowseItem) ([]int, error) {
	if len(items) == 0 {
		return nil, errors.New("tui: no items to select from")
	}
	m := newBrowseModel(title, items, 0, 0)
	res, err := runModel(m)
	if err != nil {
		return nil, err
	}
	if res.cancelled {
		return nil, ErrCancelled
	}
	if res.back {
		return nil, ErrBack
	}
	return selectedIndices(res.items, res.selected), nil
}

func newBrowseModel(title string, items []BrowseItem, width, height int) browseModel {
	m := browseModel{
		title:           title,
		items:           items,
		selected:        make(map[int]bool),
		initialSelected: make(map[int]bool),
		expanded:        make(map[int]bool),
		width:           width,
		height:          height,
	}
	for i, item := range items {
		if item.Selected {
			m.selected[i] = true
		}
	}
	m.syncAllSelection()
	m.rememberInitialSelection()
	m.ensureCursorVisible()
	return m
}

type browseModel struct {
	title           string
	items           []BrowseItem
	selected        map[int]bool
	initialSelected map[int]bool
	expanded        map[int]bool
	cursor          int
	width           int
	height          int
	scroll          int
	done            bool
	cancelled       bool
	back            bool
	confirmDiscard  bool
	err             string
}

func (m browseModel) Init() tea.Cmd { return tea.RequestWindowSize }

func (m browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureCursorVisible()
		return m, nil
	case tea.KeyPressMsg:
		if m.confirmDiscard {
			switch msg.String() {
			case "ctrl+c":
				m.cancelled = true
				return m, tea.Quit
			case "y", "Y":
				m.back = true
				return m, tea.Quit
			case "n", "N", "esc", "enter":
				m.confirmDiscard = false
				return m, nil
			}
			return m, nil
		}
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			if m.selectionChanged() {
				m.confirmDiscard = true
				return m, nil
			}
			m.back = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "right", "l":
			m.expanded[m.cursor] = true
		case "left", "h":
			m.expanded[m.cursor] = false
		case "space":
			m.toggleSelection(m.cursor)
		case "enter":
			m.done = true
			return m, tea.Quit
		}
	}
	m.ensureCursorVisible()
	return m, nil
}

func (m browseModel) selectionChanged() bool {
	for i := range m.items {
		if m.selected[i] != m.initialSelected[i] {
			return true
		}
	}
	return false
}

func (m *browseModel) rememberInitialSelection() {
	m.initialSelected = make(map[int]bool, len(m.selected))
	for i, selected := range m.selected {
		if selected {
			m.initialSelected[i] = true
		}
	}
}

func (m *browseModel) toggleSelection(idx int) {
	selected := !m.selected[idx]
	m.selected[idx] = selected
	if m.items[idx].SelectsAll {
		for i := range m.items {
			m.selected[i] = selected
		}
		return
	}
	for i, item := range m.items {
		if item.SelectsAll && !selected {
			m.selected[i] = false
			return
		}
	}
}

func (m *browseModel) syncAllSelection() {
	for i, item := range m.items {
		if !item.SelectsAll || !m.selected[i] {
			continue
		}
		for j := range m.items {
			m.selected[j] = true
		}
	}
}

func (m browseModel) chosen() []int {
	out := make([]int, 0, len(m.selected))
	for i, v := range m.selected {
		if v {
			out = append(out, i)
		}
	}
	return out
}

func (m browseModel) View() tea.View {
	var b strings.Builder
	header := statusHeader(m.title)
	nameWidth := m.browseNameWidth()
	lines := m.browseLines(nameWidth)
	viewportHeight := m.browseViewportHeightFor(header, len(lines))
	start, end := visibleRange(len(lines), m.scroll, viewportHeight)
	footer := m.browseFooter(scrollHint(start, end, len(lines)))

	b.WriteString(header)
	fmt.Fprintf(&b, "        %-*s  %s\n", nameWidth, "SKILL", "PATH")
	for _, line := range lines[start:end] {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if m.height > 0 {
		for i := end - start; i < viewportHeight; i++ {
			b.WriteByte('\n')
		}
	}
	b.WriteString(footer)
	return newStatusView(b.String())
}

func (m browseModel) browseNameWidth() int {
	nameWidth := 4
	for _, item := range m.items {
		if len(item.Name) > nameWidth {
			nameWidth = len(item.Name)
		}
	}
	return nameWidth
}

func (m browseModel) browseLines(nameWidth int) []string {
	var lines []string
	for i, item := range m.items {
		cursor := listCursor(i == m.cursor)
		check := "[ ]"
		if m.selected[i] {
			check = selectStyle.Render("[x]")
		}
		twist := "▸"
		if m.expanded[i] {
			twist = "▾"
		}
		lines = append(lines, fmt.Sprintf("%s%s %s %-*s  %s", cursor, check, twist, nameWidth, item.Name, item.Path))
		if m.expanded[i] {
			desc := strings.TrimSpace(item.Description)
			if desc == "" {
				desc = "(no description)"
			}
			lines = append(lines, wrapDescriptionLines(desc, "      ", m.width)...)
		}
	}
	return lines
}

func wrapDescriptionLines(desc, prefix string, width int) []string {
	available := width - lipgloss.Width(prefix)
	if available < 1 {
		available = 0
	}
	var lines []string
	for rawLine := range strings.SplitSeq(desc, "\n") {
		line := strings.TrimSpace(rawLine)
		if available > 0 {
			line = lipgloss.Wrap(line, available, "")
		}
		for wrapped := range strings.SplitSeq(line, "\n") {
			lines = append(lines, prefix+wrapped)
		}
	}
	return lines
}

func (m browseModel) browseFooter(scrollHint string) string {
	var b strings.Builder
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err))
	}
	if m.confirmDiscard {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("Discard selection changes and choose another repo? y/N"))
	}
	if scrollHint != "" {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render(scrollHint))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ move • ←/→ collapse/expand • space toggle • enter confirm • esc back • ctrl-c cancel"))
	return b.String()
}

func (m browseModel) browseViewportHeightFor(header string, lineCount int) int {
	footer := m.browseFooter("")
	height := m.browseViewportHeight(header, footer, lineCount)
	if m.height > 0 && lineCount > height {
		footer = m.browseFooter("↓ more skills")
		height = m.browseViewportHeight(header, footer, lineCount)
	}
	return height
}

func (m browseModel) browseViewportHeight(header, footer string, lineCount int) int {
	if m.height <= 0 {
		return lineCount
	}
	height := m.height - strings.Count(header, "\n") - 1 - lipgloss.Height(footer)
	if height < 1 {
		return 1
	}
	return height
}

func (m *browseModel) ensureCursorVisible() {
	nameWidth := m.browseNameWidth()
	lines := m.browseLines(nameWidth)
	viewportHeight := m.browseViewportHeightFor(statusHeader(m.title), len(lines))
	if viewportHeight <= 0 {
		m.scroll = 0
		return
	}

	cursorLine := m.cursorLine()
	if cursorLine < m.scroll {
		m.scroll = cursorLine
	} else if cursorLine >= m.scroll+viewportHeight {
		m.scroll = cursorLine - viewportHeight + 1
	}

	maxScroll := max(0, len(lines)-viewportHeight)
	if m.scroll > maxScroll {
		m.scroll = maxScroll
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
}

func (m browseModel) cursorLine() int {
	line := 0
	for i := 0; i < m.cursor && i < len(m.items); i++ {
		line += m.itemLineCount(i)
	}
	return line
}

func scrollHint(start, end, total int) string {
	if start <= 0 && end >= total {
		return ""
	}
	if start > 0 && end < total {
		return "↑/↓ more skills"
	}
	if end < total {
		return "↓ more skills"
	}
	return "↑ more skills"
}

func (m browseModel) itemLineCount(i int) int {
	if i < 0 || i >= len(m.items) {
		return 0
	}
	if !m.expanded[i] {
		return 1
	}
	desc := strings.TrimSpace(m.items[i].Description)
	if desc == "" {
		return 2
	}
	return 1 + strings.Count(desc, "\n") + 1
}

func visibleRange(total, scroll, height int) (int, int) {
	if height <= 0 || height >= total {
		return 0, total
	}
	if scroll < 0 {
		scroll = 0
	}
	if scroll > total-height {
		scroll = total - height
	}
	return scroll, scroll + height
}

type StatusActionKind string

const (
	StatusActionQuit         StatusActionKind = "quit"
	StatusActionSync         StatusActionKind = "sync"
	StatusActionDelete       StatusActionKind = "delete"
	StatusActionNext         StatusActionKind = "next"
	StatusActionUpdateTag    StatusActionKind = "update-tag"
	StatusActionChooseSkills StatusActionKind = "choose-skills"
	StatusActionAddRepo      StatusActionKind = "add-repo"
)

type StatusAction struct {
	Kind     StatusActionKind
	RepoID   string
	SkillID  string
	Tag      string
	URL      string
	Selected []int
}

type StatusAddRepoFunc func(string) (StatusAddRepoResult, error)

type StatusApplyFunc func(StatusAction) (StatusSnapshot, error)

type StatusAddRepoResult struct {
	URL   string
	Items []BrowseItem
}

type StatusSnapshot struct {
	Repos   []StatusRepo
	Message string
}

type StatusRepo struct {
	ID          string
	Name        string
	Version     string
	Upgrade     bool
	Checking    bool
	Error       string
	BrowseError string
	Tags        []StatusTag
	Skills      []StatusSkill
	BrowseItems []BrowseItem
}

type StatusTag struct {
	Name    string
	Created string
}

type StatusSkill struct {
	ID          string
	Name        string
	Path        string
	SourceDir   string
	Description string
	Status      string
}

func RunStatus(title string, snapshot StatusSnapshot, update func() StatusSnapshot, addRepo StatusAddRepoFunc) (StatusAction, error) {
	return runStatusModel(newStatusModel(title, snapshot, update, addRepo))
}

func RunInteractiveStatus(
	title string,
	snapshot StatusSnapshot,
	update func() StatusSnapshot,
	addRepo StatusAddRepoFunc,
	apply StatusApplyFunc,
) error {
	m := newStatusModelWithApply(title, snapshot, update, addRepo, apply)
	_, err := runStatusModel(m)
	if errors.Is(err, ErrCancelled) {
		return nil
	}
	return err
}

func runStatusModel(m statusModel) (StatusAction, error) {
	res, err := runModel(m)
	if err != nil {
		return StatusAction{}, err
	}
	if res.cancelled {
		return StatusAction{}, ErrCancelled
	}
	if res.action.Kind == "" {
		res.action.Kind = StatusActionQuit
	}
	return res.action, nil
}

type statusUpdateMsg StatusSnapshot

type statusAddRepoMsg struct {
	result StatusAddRepoResult
	err    error
}

type statusApplyMsg struct {
	snapshot StatusSnapshot
	err      error
}

type statusRow struct {
	kind  string
	repo  int
	skill int
}

type statusModel struct {
	title         string
	snapshot      StatusSnapshot
	rows          []statusRow
	cursor        int
	width         int
	height        int
	expanded      map[string]bool
	action        StatusAction
	cancelled     bool
	confirmDelete bool
	tagSelect     bool
	tagCursor     int
	addRepo       *textModel
	addRepoFunc   StatusAddRepoFunc
	addRepoStatus string
	addRepoURL    string
	apply         StatusApplyFunc
	applying      bool
	browse        *browseModel
	browseRepoID  string
	err           string
	update        func() StatusSnapshot
}

func newStatusModel(title string, snapshot StatusSnapshot, update func() StatusSnapshot, addRepo ...StatusAddRepoFunc) statusModel {
	m := statusModel{title: title, snapshot: snapshot, expanded: map[string]bool{}, update: update}
	if len(addRepo) > 0 {
		m.addRepoFunc = addRepo[0]
	}
	m.rebuildRows()
	return m
}

func newStatusModelWithApply(
	title string,
	snapshot StatusSnapshot,
	update func() StatusSnapshot,
	addRepo StatusAddRepoFunc,
	apply StatusApplyFunc,
) statusModel {
	m := newStatusModel(title, snapshot, update, addRepo)
	m.apply = apply
	return m
}

func (m *statusModel) rebuildRows() {
	m.rows = nil
	for i, repo := range m.snapshot.Repos {
		m.rows = append(m.rows, statusRow{kind: "repo", repo: i})
		for j := range repo.Skills {
			m.rows = append(m.rows, statusRow{kind: "skill", repo: i, skill: j})
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m statusModel) Init() tea.Cmd {
	return waitForStatusUpdate(m.update)
}

func waitForStatusUpdate(update func() StatusSnapshot) tea.Cmd {
	if update == nil {
		return nil
	}
	return func() tea.Msg {
		return statusUpdateMsg(update())
	}
}

func (m statusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.browse != nil {
			m.browse.width = msg.Width
			m.browse.height = msg.Height
			m.browse.ensureCursorVisible()
		}
		return m, nil
	case statusUpdateMsg:
		m.snapshot = StatusSnapshot(msg)
		m.update = nil
		m.rebuildRows()
		return m, nil
	case statusAddRepoMsg:
		m.addRepoStatus = ""
		if msg.err != nil {
			if m.addRepo != nil {
				m.addRepo.err = msg.err.Error()
			}
			return m, nil
		}
		if len(msg.result.Items) == 0 {
			if m.addRepo != nil {
				m.addRepo.err = "no skills available for this repo"
			}
			return m, nil
		}
		m.addRepo = nil
		m.addRepoURL = msg.result.URL
		browse := newBrowseModel("Select skills to add:", msg.result.Items, m.width, m.height)
		m.browse = &browse
		m.err = ""
		return m, nil
	case statusApplyMsg:
		m.applying = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.snapshot = msg.snapshot
		m.rebuildRows()
		m.err = ""
		return m, nil
	case tea.PasteMsg:
		if m.addRepo != nil {
			return m.updateAddRepo(msg)
		}
		return m, nil
	case tea.KeyPressMsg:
		if m.applying {
			return m, nil
		}
		if m.addRepo != nil {
			return m.updateAddRepo(msg)
		}
		if m.browse != nil {
			return m.updateBrowse(msg)
		}
		if m.confirmDelete {
			switch msg.String() {
			case "ctrl+c":
				m.cancelled = true
				return m, tea.Quit
			case "y", "Y":
				row := m.rows[m.cursor]
				repo := m.snapshot.Repos[row.repo]
				skill := repo.Skills[row.skill]
				return m.finishStatusAction(StatusAction{Kind: StatusActionDelete, RepoID: repo.ID, SkillID: skill.ID})
			case "n", "N", "esc", "enter":
				m.confirmDelete = false
				return m, nil
			}
			return m, nil
		}
		if m.tagSelect {
			return m.updateTagSelect(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "q", "esc":
			m.action = StatusAction{Kind: StatusActionQuit}
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "right", "l":
			_, skill, ok := m.currentSkill()
			if ok {
				if m.expanded == nil {
					m.expanded = map[string]bool{}
				}
				m.expanded[skill.ID] = true
			}
		case "left", "h":
			_, skill, ok := m.currentSkill()
			if ok {
				m.expanded[skill.ID] = false
			}
		case "a":
			if _, ok := m.currentRepo(); ok {
				addRepo := newTextModel("Add skills repo", "Enter the git URL of a skills repo:", "github.com/owner/skills")
				m.addRepo = &addRepo
				m.addRepoURL = ""
				m.addRepoStatus = ""
				m.browseRepoID = ""
				m.err = ""
				return m, m.addRepo.Init()
			}
		case "c":
			if repo, ok := m.currentRepo(); ok {
				if repo.BrowseError != "" {
					m.err = repo.BrowseError
					return m, nil
				}
				if len(repo.BrowseItems) == 0 {
					m.err = "no skills available for this repo"
					return m, nil
				}
				browse := newBrowseModel("Select skills to add:", repo.BrowseItems, m.width, m.height)
				m.browse = &browse
				m.browseRepoID = repo.ID
				m.err = ""
				return m, nil
			}
		case "t":
			if repo, ok := m.currentRepo(); ok {
				if repo.Checking {
					m.err = "still checking this repo for tags"
					return m, nil
				}
				if repo.Error != "" {
					m.err = "could not check this repo: " + repo.Error
					return m, nil
				}
				if len(repo.Tags) == 0 {
					m.err = "no tags available for this repo"
					return m, nil
				}
				m.tagSelect = true
				m.tagCursor = 0
				m.err = ""
			}
		case "u":
			if repo, ok := m.currentRepo(); ok {
				if repo.Version != "" && repo.Checking {
					m.err = "still checking this repo for updates"
					return m, nil
				}
				if repo.Version != "" && repo.Error != "" {
					m.err = "could not check this repo: " + repo.Error
					return m, nil
				}
				action := StatusAction{Kind: StatusActionNext, RepoID: repo.ID}
				if repo.Version != "" && repo.Upgrade && len(repo.Tags) > 0 {
					action.Tag = repo.Tags[0].Name
				}
				return m.finishStatusAction(action)
			}
		case "s":
			if repo, skill, ok := m.currentSkill(); ok {
				return m.finishStatusAction(StatusAction{Kind: StatusActionSync, RepoID: repo.ID, SkillID: skill.ID})
			}
		case "d":
			if _, _, ok := m.currentSkill(); ok {
				m.confirmDelete = true
				m.err = ""
			}
		}
	}
	return m, nil
}

func (m statusModel) finishStatusAction(action StatusAction) (tea.Model, tea.Cmd) {
	if m.apply == nil {
		m.action = action
		return m, tea.Quit
	}
	m.confirmDelete = false
	m.tagSelect = false
	m.applying = true
	m.err = ""
	return m, runStatusApply(m.apply, action)
}

func (m statusModel) updateBrowse(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	updated, _ := m.browse.Update(msg)
	browse := updated.(browseModel)
	m.browse = &browse
	switch {
	case browse.cancelled:
		m.cancelled = true
		return m, tea.Quit
	case browse.back:
		m.browse = nil
		m.browseRepoID = ""
		m.addRepoURL = ""
		return m, nil
	case browse.done:
		kind := StatusActionChooseSkills
		if m.addRepoURL != "" {
			kind = StatusActionAddRepo
		}
		action := StatusAction{
			Kind:     kind,
			RepoID:   m.browseRepoID,
			URL:      m.addRepoURL,
			Selected: selectedIndices(browse.items, browse.selected),
		}
		if m.apply != nil {
			m.browse = nil
			m.browseRepoID = ""
			m.addRepoURL = ""
			m.applying = true
			m.err = ""
			return m, runStatusApply(m.apply, action)
		}
		m.action = action
		return m, tea.Quit
	default:
		return m, nil
	}
}

func runStatusApply(apply StatusApplyFunc, action StatusAction) tea.Cmd {
	return func() tea.Msg {
		snapshot, err := apply(action)
		return statusApplyMsg{snapshot: snapshot, err: err}
	}
}

func (m statusModel) updateAddRepo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.addRepoStatus != "" {
		return m, nil
	}
	if msg, ok := msg.(tea.KeyPressMsg); ok {
		switch msg.String() {
		case "ctrl+c":
			m.cancelled = true
			return m, tea.Quit
		case "esc":
			m.addRepo = nil
			return m, nil
		}
	}
	updated, cmd := m.addRepo.Update(msg)
	addRepo := updated.(textModel)
	m.addRepo = &addRepo
	if addRepo.done {
		if m.addRepoFunc == nil {
			m.action = StatusAction{Kind: StatusActionAddRepo, URL: addRepo.value}
			return m, tea.Quit
		}
		m.addRepoStatus = fmt.Sprintf("Cloning %s ...", addRepo.value)
		return m, runStatusAddRepo(m.addRepoFunc, addRepo.value)
	}
	return m, cmd
}

func runStatusAddRepo(addRepo StatusAddRepoFunc, rawURL string) tea.Cmd {
	return func() tea.Msg {
		result, err := addRepo(rawURL)
		return statusAddRepoMsg{result: result, err: err}
	}
}

func (m statusModel) updateTagSelect(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	repo, ok := m.currentRepo()
	if !ok {
		m.tagSelect = false
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		m.cancelled = true
		return m, tea.Quit
	case "esc":
		m.tagSelect = false
	case "up", "k":
		if m.tagCursor > 0 {
			m.tagCursor--
		}
	case "down", "j":
		if m.tagCursor < len(repo.Tags)-1 {
			m.tagCursor++
		}
	case "enter":
		tag := repo.Tags[m.tagCursor]
		return m.finishStatusAction(StatusAction{Kind: StatusActionUpdateTag, RepoID: repo.ID, Tag: tag.Name})
	}
	return m, nil
}

func (m statusModel) currentRepo() (StatusRepo, bool) {
	if len(m.rows) == 0 {
		return StatusRepo{}, false
	}
	row := m.rows[m.cursor]
	return m.snapshot.Repos[row.repo], row.kind == "repo"
}

func (m statusModel) currentSkill() (StatusRepo, StatusSkill, bool) {
	if len(m.rows) == 0 {
		return StatusRepo{}, StatusSkill{}, false
	}
	row := m.rows[m.cursor]
	if row.kind != "skill" {
		return StatusRepo{}, StatusSkill{}, false
	}
	repo := m.snapshot.Repos[row.repo]
	return repo, repo.Skills[row.skill], true
}

func statusHeader(title string) string {
	var b strings.Builder
	b.WriteString(renderStatusLogo())
	b.WriteString("\n")
	b.WriteString("A tool for syncing skills across repositories.\n")
	b.WriteString("Run skink -h to show command line usage.")
	if title != "" {
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render(title))
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n\n")
	}
	return b.String()
}

func writeStatusHeader(b *strings.Builder, title string) {
	b.WriteString(statusHeader(title))
}

func listCursor(selected bool) string {
	if selected {
		return cursorStyle.Render(cursorMarker)
	}
	return strings.Repeat(" ", lipgloss.Width(cursorMarker))
}

func (m statusModel) View() tea.View {
	if m.addRepo != nil {
		return newStatusView(m.addRepoView())
	}
	if m.browse != nil {
		return m.browse.View()
	}
	var b strings.Builder
	writeStatusHeader(&b, m.title)
	if len(m.snapshot.Repos) == 0 {
		b.WriteString("No configured skills.\n")
		return newStatusView(b.String())
	}
	if m.tagSelect {
		return newStatusView(m.tagSelectView())
	}
	rowIndex := 0
	for _, repo := range m.snapshot.Repos {
		cursor := listCursor(rowIndex == m.cursor)
		prefix := ""
		suffix := ""
		if repo.Upgrade {
			prefix = " ⬆️"
		}
		if repo.Checking {
			prefix = ""
			suffix = helpStyle.Render(" checking...")
		} else if repo.Error != "" {
			prefix = errStyle.Render(" check failed")
		}
		version := repo.Version
		if version == "" {
			version = "HEAD"
		}
		fmt.Fprintf(&b, "%s%s%s (%s)%s\n", cursor, titleStyle.Render(repo.Name), prefix, version, suffix)
		if repo.Error != "" {
			fmt.Fprintf(&b, "    %s\n", errStyle.Render(repo.Error))
		}
		rowIndex++
		for _, skill := range repo.Skills {
			cursor := listCursor(rowIndex == m.cursor)
			twist := "▸"
			if m.expanded[skill.ID] {
				twist = "▾"
			}
			fmt.Fprintf(&b, "%s  %s %s %-18s %s\n", cursor, paddedStatusEmoji(skill.Status), twist, skill.Name, skill.Path)
			if m.expanded[skill.ID] {
				desc := strings.TrimSpace(skill.Description)
				if desc == "" {
					desc = "(no description)"
				}
				for _, line := range wrapDescriptionLines(desc, "      ", m.width) {
					b.WriteString(line)
					b.WriteString("\n")
				}
			}
			rowIndex++
		}
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("Key: ✅ up to date • ⚠️ different • ❌ missing • ⬆️ update available"))
	b.WriteString("\n")
	if m.snapshot.Message != "" {
		b.WriteString("\n")
		b.WriteString(m.snapshot.Message)
		b.WriteString("\n")
	}
	if m.applying {
		b.WriteString("\n")
		b.WriteString(helpStyle.Render("Applying changes..."))
		b.WriteString("\n")
	}
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err))
		b.WriteString("\n")
	}
	if m.confirmDelete {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("Delete this skill locally and remove it from .skink config? y/N"))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("move: ↑/↓ • q/esc quit"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("repo: a add repo, c choose skills, t choose tag, u update newest/head"))
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("skills: ←/→ collapse/expand, s sync/overwrite, d delete"))
	return newStatusView(b.String())
}

func (m statusModel) addRepoView() string {
	var b strings.Builder
	addRepo := m.addRepo
	writeStatusHeader(&b, addRepo.title)
	b.WriteString(addRepo.prompt)
	b.WriteString("\n")
	b.WriteString(addRepo.input.View())
	b.WriteString("\n")
	if addRepo.err != "" {
		b.WriteString(errStyle.Render(addRepo.err))
		b.WriteString("\n")
	}
	if m.addRepoStatus != "" {
		b.WriteString(helpStyle.Render(m.addRepoStatus))
	} else {
		b.WriteString(helpStyle.Render("enter to confirm • esc back • ctrl-c cancel"))
	}
	return b.String()
}

func (m statusModel) tagSelectView() string {
	var b strings.Builder
	repo, _ := m.currentRepo()
	writeStatusHeader(&b, "Choose tag for "+repo.Name)
	for i, tag := range repo.Tags {
		cursor := listCursor(i == m.tagCursor)
		created := tag.Created
		if created != "" {
			created = "  " + created
		}
		fmt.Fprintf(&b, "%s%s%s\n", cursor, tag.Name, created)
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ move • enter choose • esc back"))
	return b.String()
}

func paddedStatusEmoji(status string) string {
	emoji := statusEmoji(status)
	if status == "different" {
		emoji += " "
	}
	for lipgloss.Width(emoji) < 2 {
		emoji += " "
	}
	return emoji
}

func statusEmoji(status string) string {
	switch status {
	case "missing":
		return "❌"
	case "different":
		return "⚠️"
	case "up to date":
		return "✅"
	default:
		return "?"
	}
}
