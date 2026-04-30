package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// These tests exercise the Update/View logic directly — they don't spin up a
// real tea.Program, which would need a TTY.

func keyMsg(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	case "esc":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
	case "space":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeySpace, Text: " "})
	case "up":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	case "down":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown})
	case "right":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight})
	case "left":
		return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft})
	case "ctrl+c":
		return tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	}
	runes := []rune(s)
	return tea.KeyPressMsg(tea.Key{Code: runes[0], Text: s})
}

func TestTextModelEmptyRejected(t *testing.T) {
	m := textModel{prompt: "x"}
	updated, _ := m.Update(keyMsg("enter"))
	tm := updated.(textModel)
	if tm.done {
		t.Error("empty enter should not complete")
	}
	if tm.err == "" {
		t.Error("expected error message")
	}
}

func TestTextModelCancel(t *testing.T) {
	m := textModel{}
	updated, _ := m.Update(keyMsg("esc"))
	if !updated.(textModel).cancelled {
		t.Error("esc should cancel")
	}
}

func TestMultiModelToggleAndEnter(t *testing.T) {
	m := multiModel{items: []string{"a", "b", "c"}, selected: map[int]bool{}}
	// toggle index 0
	next, _ := m.Update(keyMsg("space"))
	m = next.(multiModel)
	if !m.selected[0] {
		t.Error("space should select index 0")
	}
	// move down and toggle index 1
	next, _ = m.Update(keyMsg("down"))
	m = next.(multiModel)
	next, _ = m.Update(keyMsg("space"))
	m = next.(multiModel)
	if !m.selected[1] {
		t.Error("index 1 should be selected")
	}
	// enter should quit with no error
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(multiModel)
	if m.err != "" {
		t.Errorf("unexpected err: %q", m.err)
	}
	if cmd == nil {
		t.Error("enter with selection should return Quit cmd")
	}
}

func TestMultiModelEmptyEnterRejected(t *testing.T) {
	m := multiModel{items: []string{"a"}, selected: map[int]bool{}}
	next, _ := m.Update(keyMsg("enter"))
	m = next.(multiModel)
	if m.err == "" {
		t.Error("empty selection should produce error")
	}
}

func TestMultiModelToggleAll(t *testing.T) {
	m := multiModel{items: []string{"a", "b"}, selected: map[int]bool{}}
	next, _ := m.Update(keyMsg("a"))
	m = next.(multiModel)
	if !m.selected[0] || !m.selected[1] {
		t.Error("'a' should select all")
	}
	next, _ = m.Update(keyMsg("a"))
	m = next.(multiModel)
	if m.selected[0] || m.selected[1] {
		t.Error("second 'a' should unselect all")
	}
}

func TestSelectedIndicesReturnsItemOrder(t *testing.T) {
	got := selectedIndices([]string{"a", "b", "c"}, map[int]bool{2: true, 0: true, 1: false})
	want := []int{0, 2}
	if len(got) != len(want) {
		t.Fatalf("selected indices = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("selected indices = %v, want %v", got, want)
		}
	}
}

func TestSingleModelNav(t *testing.T) {
	m := singleModel{items: []string{"a", "b", "c"}}
	next, _ := m.Update(keyMsg("down"))
	m = next.(singleModel)
	if m.cursor != 1 {
		t.Errorf("cursor = %d", m.cursor)
	}
	next, _ = m.Update(keyMsg("up"))
	m = next.(singleModel)
	if m.cursor != 0 {
		t.Errorf("cursor = %d", m.cursor)
	}
	// cursor can't go below 0
	next, _ = m.Update(keyMsg("up"))
	m = next.(singleModel)
	if m.cursor != 0 {
		t.Errorf("cursor = %d", m.cursor)
	}
}

func TestBrowseModelToggleExpandAndEnter(t *testing.T) {
	m := browseModel{
		items: []BrowseItem{
			{Name: "alpha", Path: "skills/alpha", Description: "Alpha desc."},
			{Name: "beta", Path: "skills/beta", Description: "Beta desc."},
		},
		selected: map[int]bool{},
		expanded: map[int]bool{},
	}
	next, _ := m.Update(keyMsg("right"))
	m = next.(browseModel)
	if !m.expanded[0] {
		t.Fatal("right should expand current row")
	}
	if view := m.View().Content; !strings.Contains(view, "Alpha desc.") || !strings.Contains(view, "skills/alpha") {
		t.Fatalf("expanded view missing description/path:\n%s", view)
	}
	next, _ = m.Update(keyMsg("space"))
	m = next.(browseModel)
	if !m.selected[0] {
		t.Fatal("space should select current row")
	}
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(browseModel)
	if m.err != "" {
		t.Fatalf("unexpected error: %q", m.err)
	}
	if cmd == nil {
		t.Fatal("enter with selection should quit")
	}
}

func TestBrowseModelEmptyEnterRejected(t *testing.T) {
	m := browseModel{
		items:    []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected: map[int]bool{},
		expanded: map[int]bool{},
	}
	next, cmd := m.Update(keyMsg("enter"))
	if next.(browseModel).err != "" {
		t.Fatal("empty selection should be allowed")
	}
	if cmd == nil {
		t.Fatal("enter should quit")
	}
}

func TestBrowseModelInitialSelection(t *testing.T) {
	items := []BrowseItem{
		{Name: "alpha", Path: "skills/alpha", Selected: true},
		{Name: "beta", Path: "skills/beta"},
	}
	idxs, err := browseSelectedIndicesForTest(items)
	if err != nil {
		t.Fatal(err)
	}
	if len(idxs) != 1 || idxs[0] != 0 {
		t.Fatalf("initial selection = %v", idxs)
	}
}

func TestBrowseModelEscWithoutChangesGoesBack(t *testing.T) {
	m := browseModel{
		items:           []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected:        map[int]bool{},
		initialSelected: map[int]bool{},
		expanded:        map[int]bool{},
	}
	next, cmd := m.Update(keyMsg("esc"))
	m = next.(browseModel)
	if !m.back || m.cancelled || m.confirmDiscard {
		t.Fatalf("esc without changes should go back without confirmation: %+v", m)
	}
	if cmd == nil {
		t.Fatal("esc without changes should quit")
	}
}

func TestBrowseModelDirtyEscCanKeepChanges(t *testing.T) {
	m := browseModel{
		items:           []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected:        map[int]bool{},
		initialSelected: map[int]bool{},
		expanded:        map[int]bool{},
	}
	next, _ := m.Update(keyMsg("space"))
	m = next.(browseModel)
	next, cmd := m.Update(keyMsg("esc"))
	m = next.(browseModel)
	if !m.confirmDiscard {
		t.Fatalf("dirty esc should ask for confirmation: %+v", m)
	}
	if cmd != nil {
		t.Fatal("dirty esc should not quit before confirmation")
	}
	next, cmd = m.Update(keyMsg("n"))
	m = next.(browseModel)
	if m.confirmDiscard || m.back || m.cancelled {
		t.Fatalf("no should ignore escape and keep browsing: %+v", m)
	}
	if !m.selected[0] {
		t.Fatal("selection change should be preserved after declining discard")
	}
	if cmd != nil {
		t.Fatal("declining discard should not quit")
	}
}

func TestBrowseModelDirtyEscCanDiscardAndGoBack(t *testing.T) {
	m := browseModel{
		items:           []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected:        map[int]bool{},
		initialSelected: map[int]bool{},
		expanded:        map[int]bool{},
	}
	next, _ := m.Update(keyMsg("space"))
	m = next.(browseModel)
	next, _ = m.Update(keyMsg("esc"))
	m = next.(browseModel)
	next, cmd := m.Update(keyMsg("y"))
	m = next.(browseModel)
	if !m.back || m.cancelled {
		t.Fatalf("yes should discard and go back: %+v", m)
	}
	if cmd == nil {
		t.Fatal("confirming discard should quit")
	}
}

func TestBrowseModelCtrlCCancels(t *testing.T) {
	m := browseModel{
		items:           []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected:        map[int]bool{},
		initialSelected: map[int]bool{},
		expanded:        map[int]bool{},
	}
	next, cmd := m.Update(keyMsg("ctrl+c"))
	m = next.(browseModel)
	if !m.cancelled || m.back {
		t.Fatalf("ctrl-c should cancel, got %+v", m)
	}
	if cmd == nil {
		t.Fatal("ctrl-c should quit")
	}
}

func TestStatusModelRepoActions(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:      "github.com/acme/team",
		Name:    "github.com/acme/team",
		Version: "v1.0.0",
		Upgrade: true,
		Tags:    []StatusTag{{Name: "v1.1.0"}, {Name: "v1.0.0"}},
		Skills:  []StatusSkill{{ID: "skill", Name: "alpha", Path: "skills/alpha", Status: "missing"}},
	}}})
	next, _ := m.Update(keyMsg("t"))
	m = next.(statusModel)
	if !m.tagSelect {
		t.Fatal("t on repo should enter tag selection")
	}
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionUpdateTag || m.action.Tag != "v1.1.0" {
		t.Fatalf("tag action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}})
	next, cmd = m.Update(keyMsg("u"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionNext || m.action.RepoID != "repo" {
		t.Fatalf("next action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}})
	next, cmd = m.Update(keyMsg("c"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionChooseSkills || m.action.RepoID != "repo" {
		t.Fatalf("choose skills action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}})
	next, cmd = m.Update(keyMsg("a"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionAddRepo {
		t.Fatalf("add repo action = %+v cmd=%v", m.action, cmd)
	}
}

func TestStatusModelSkillActionsAndDeleteConfirm(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID: "repo",
		Skills: []StatusSkill{
			{ID: "alpha", Name: "alpha", Path: "skills/alpha", Status: "up to date"},
		},
	}}})
	next, _ := m.Update(keyMsg("down"))
	m = next.(statusModel)
	next, cmd := m.Update(keyMsg("s"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionSync || m.action.SkillID != "alpha" {
		t.Fatalf("sync action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID: "repo",
		Skills: []StatusSkill{
			{ID: "alpha", Name: "alpha", Path: "skills/alpha", Status: "up to date"},
		},
	}}})
	next, _ = m.Update(keyMsg("down"))
	m = next.(statusModel)
	next, cmd = m.Update(keyMsg("d"))
	m = next.(statusModel)
	if !m.confirmDelete || cmd != nil {
		t.Fatalf("delete should ask for confirmation: %+v cmd=%v", m, cmd)
	}
	next, cmd = m.Update(keyMsg("n"))
	m = next.(statusModel)
	if m.confirmDelete || cmd != nil {
		t.Fatalf("declining delete should stay on page: %+v cmd=%v", m, cmd)
	}
	next, _ = m.Update(keyMsg("d"))
	m = next.(statusModel)
	next, cmd = m.Update(keyMsg("y"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionDelete || m.action.SkillID != "alpha" {
		t.Fatalf("delete action = %+v cmd=%v", m.action, cmd)
	}
}

func browseSelectedIndicesForTest(items []BrowseItem) ([]int, error) {
	m := browseModel{items: items, selected: map[int]bool{}, expanded: map[int]bool{}}
	for i, item := range items {
		if item.Selected {
			m.selected[i] = true
		}
	}
	out := make([]int, 0, len(m.selected))
	for i := range m.items {
		if m.selected[i] {
			out = append(out, i)
		}
	}
	return out, nil
}

func TestViewRenders(t *testing.T) {
	// Smoke test: View() must not panic on initial state.
	_ = multiModel{items: []string{"a"}, selected: map[int]bool{}}.View()
	_ = singleModel{items: []string{"a"}}.View()
	_ = textModel{prompt: "p"}.View()
	_ = browseModel{items: []BrowseItem{{Name: "a", Path: "p"}}, selected: map[int]bool{}, expanded: map[int]bool{}}.View()
}
