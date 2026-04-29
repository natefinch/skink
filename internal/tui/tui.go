// Package tui holds the Bubble Tea UI models used by skink. Each exported
// Run* function drives one interactive screen and returns the user's choice
// or an error. Higher layers (cli) call these; lower layers (config,
// installer, etc.) know nothing about this package.
package tui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrCancelled is returned when the user presses ctrl-c or esc.
var ErrCancelled = errors.New("tui: cancelled by user")

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	cursorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EE6FF8"))
	selectStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#04B575"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
)

// RunTextPrompt shows a single-line text input with the given prompt and
// returns the entered (trimmed) value. An empty value is rejected.
func RunTextPrompt(title, prompt, placeholder string) (string, error) {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.Focus()
	ti.CharLimit = 512
	ti.Width = 64

	m := textModel{title: title, prompt: prompt, input: ti}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return "", err
	}
	res := final.(textModel)
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
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
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

func (m textModel) View() string {
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
	return b.String()
}

// RunMultiSelect shows a list of items with checkboxes. Returns the indices
// selected (sorted). An empty selection is rejected unless allowEmpty is
// true.
func RunMultiSelect(title string, items []string, allowEmpty bool) ([]int, error) {
	if len(items) == 0 {
		return nil, errors.New("tui: no items to select from")
	}
	m := multiModel{title: title, items: items, allowEmpty: allowEmpty, selected: make(map[int]bool)}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, err
	}
	res := final.(multiModel)
	if res.cancelled {
		return nil, ErrCancelled
	}
	out := make([]int, 0, len(res.selected))
	for i := range res.items {
		if res.selected[i] {
			out = append(out, i)
		}
	}
	return out, nil
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
	case tea.KeyMsg:
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
		case " ":
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

func (m multiModel) View() string {
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
	return b.String()
}

// RunSingleSelect shows a list and returns the chosen index.
func RunSingleSelect(title string, items []string) (int, error) {
	if len(items) == 0 {
		return 0, errors.New("tui: no items to select from")
	}
	m := singleModel{title: title, items: items}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return 0, err
	}
	res := final.(singleModel)
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
	case tea.KeyMsg:
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
func (m singleModel) View() string {
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
	return b.String()
}
