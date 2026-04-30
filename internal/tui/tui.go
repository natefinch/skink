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
	ti := textinput.New()
	ti.Placeholder = placeholder
	_ = ti.Focus()
	ti.CharLimit = 512
	ti.SetWidth(64)

	m := textModel{title: title, prompt: prompt, input: ti}
	res, err := runModel(m)
	if err != nil {
		return "", err
	}
	if res.cancelled {
		return "", ErrCancelled
	}
	return strings.TrimSpace(res.value), nil
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
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("❯ ")
		}
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
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("❯ ")
		}
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
}

// RunBrowseSelect shows an expandable multi-select list of discovered skills.
func RunBrowseSelect(title string, items []BrowseItem) ([]int, error) {
	if len(items) == 0 {
		return nil, errors.New("tui: no items to select from")
	}
	m := browseModel{
		title:           title,
		items:           items,
		selected:        make(map[int]bool),
		initialSelected: make(map[int]bool),
		expanded:        make(map[int]bool),
	}
	for i, item := range items {
		if item.Selected {
			m.selected[i] = true
			m.initialSelected[i] = true
		}
	}
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

type browseModel struct {
	title           string
	items           []BrowseItem
	selected        map[int]bool
	initialSelected map[int]bool
	expanded        map[int]bool
	cursor          int
	cancelled       bool
	back            bool
	confirmDiscard  bool
	err             string
}

func (m browseModel) Init() tea.Cmd { return nil }

func (m browseModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "enter":
			return m, tea.Quit
		}
	}
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
	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}
	nameWidth := 4
	for _, item := range m.items {
		if len(item.Name) > nameWidth {
			nameWidth = len(item.Name)
		}
	}
	fmt.Fprintf(&b, "    %-*s  %s\n", nameWidth, "SKILL", "PATH")
	for i, item := range m.items {
		cursor := "  "
		if i == m.cursor {
			cursor = cursorStyle.Render("❯ ")
		}
		check := "[ ]"
		if m.selected[i] {
			check = selectStyle.Render("[x]")
		}
		twist := "▸"
		if m.expanded[i] {
			twist = "▾"
		}
		fmt.Fprintf(&b, "%s%s %s %-*s  %s\n", cursor, check, twist, nameWidth, item.Name, item.Path)
		if m.expanded[i] {
			desc := strings.TrimSpace(item.Description)
			if desc == "" {
				desc = "(no description)"
			}
			for _, line := range strings.Split(desc, "\n") {
				fmt.Fprintf(&b, "      %s\n", line)
			}
		}
	}
	if m.err != "" {
		b.WriteString("\n")
		b.WriteString(errStyle.Render(m.err))
	}
	if m.confirmDiscard {
		b.WriteString("\n")
		b.WriteString(errStyle.Render("Discard selection changes and choose another repo? y/N"))
	}
	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑/↓ move • ←/→ collapse/expand • space toggle • enter confirm • esc back • ctrl-c cancel"))
	return newView(b.String())
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
	Kind    StatusActionKind
	RepoID  string
	SkillID string
	Tag     string
}

type StatusSnapshot struct {
	Repos   []StatusRepo
	Message string
}

type StatusRepo struct {
	ID      string
	Name    string
	Version string
	Upgrade bool
	Tags    []StatusTag
	Skills  []StatusSkill
}

type StatusTag struct {
	Name    string
	Created string
}

type StatusSkill struct {
	ID        string
	Name      string
	Path      string
	SourceDir string
	Status    string
}

func RunStatus(title string, snapshot StatusSnapshot) (StatusAction, error) {
	m := newStatusModel(title, snapshot)
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
	action        StatusAction
	cancelled     bool
	confirmDelete bool
	tagSelect     bool
	tagCursor     int
	err           string
}

func newStatusModel(title string, snapshot StatusSnapshot) statusModel {
	m := statusModel{title: title, snapshot: snapshot}
	m.rebuildRows()
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

func (m statusModel) Init() tea.Cmd { return nil }

func (m statusModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.confirmDelete {
			switch msg.String() {
			case "ctrl+c":
				m.cancelled = true
				return m, tea.Quit
			case "y", "Y":
				row := m.rows[m.cursor]
				repo := m.snapshot.Repos[row.repo]
				skill := repo.Skills[row.skill]
				m.action = StatusAction{Kind: StatusActionDelete, RepoID: repo.ID, SkillID: skill.ID}
				return m, tea.Quit
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
		case "a":
			if _, ok := m.currentRepo(); ok {
				m.action = StatusAction{Kind: StatusActionAddRepo}
				return m, tea.Quit
			}
		case "c":
			if repo, ok := m.currentRepo(); ok {
				m.action = StatusAction{Kind: StatusActionChooseSkills, RepoID: repo.ID}
				return m, tea.Quit
			}
		case "t":
			if repo, ok := m.currentRepo(); ok {
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
				m.action = StatusAction{Kind: StatusActionNext, RepoID: repo.ID}
				return m, tea.Quit
			}
		case "s":
			if repo, skill, ok := m.currentSkill(); ok {
				m.action = StatusAction{Kind: StatusActionSync, RepoID: repo.ID, SkillID: skill.ID}
				return m, tea.Quit
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
		m.action = StatusAction{Kind: StatusActionUpdateTag, RepoID: repo.ID, Tag: tag.Name}
		return m, tea.Quit
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

func (m statusModel) View() tea.View {
	var b strings.Builder
	if m.title != "" {
		b.WriteString(titleStyle.Render(m.title))
		b.WriteString("\n\n")
	}
	if len(m.snapshot.Repos) == 0 {
		b.WriteString("No configured skills.\n")
		return newView(b.String())
	}
	if m.tagSelect {
		return newView(m.tagSelectView())
	}
	rowIndex := 0
	for _, repo := range m.snapshot.Repos {
		cursor := "  "
		if rowIndex == m.cursor {
			cursor = cursorStyle.Render("❯ ")
		}
		upgrade := ""
		if repo.Upgrade {
			upgrade = " ⬆️"
		}
		version := repo.Version
		if version == "" {
			version = "HEAD"
		}
		fmt.Fprintf(&b, "%s%s%s (%s)\n", cursor, titleStyle.Render(repo.Name), upgrade, version)
		rowIndex++
		for _, skill := range repo.Skills {
			cursor := "  "
			if rowIndex == m.cursor {
				cursor = cursorStyle.Render("❯ ")
			}
			fmt.Fprintf(&b, "%s  %s %-18s %s\n", cursor, statusEmoji(skill.Status), skill.Name, skill.Path)
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
	b.WriteString(helpStyle.Render("↑/↓ move • repo: a add repo, c choose skills, t choose tag, u update newest/head • skill: s sync/overwrite, d delete • q/esc quit"))
	return newView(b.String())
}

func (m statusModel) tagSelectView() string {
	var b strings.Builder
	repo, _ := m.currentRepo()
	b.WriteString(titleStyle.Render("Choose tag for " + repo.Name))
	b.WriteString("\n\n")
	for i, tag := range repo.Tags {
		cursor := "  "
		if i == m.tagCursor {
			cursor = cursorStyle.Render("❯ ")
		}
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
