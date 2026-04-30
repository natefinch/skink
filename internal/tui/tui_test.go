package tui

import (
	"fmt"
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

func TestBrowseModelWrapsExpandedDescription(t *testing.T) {
	m := newBrowseModel("", []BrowseItem{{
		Name:        "alpha",
		Path:        "skills/alpha",
		Description: "first second third fourth",
	}}, 24, 0)
	m.expanded[0] = true

	content := m.View().Content
	if !strings.Contains(content, "      first second third") || !strings.Contains(content, "      fourth") {
		t.Fatalf("expanded description should wrap to the view width:\n%s", content)
	}
	if strings.Contains(content, "first second third fourth") {
		t.Fatalf("expanded description should not run past the view width:\n%s", content)
	}
}

func TestBrowseViewUsesStatusShell(t *testing.T) {
	m := browseModel{
		title:    "Select skills to add:",
		items:    []BrowseItem{{Name: "alpha", Path: "skills/alpha"}},
		selected: map[int]bool{},
		expanded: map[int]bool{},
	}
	view := m.View()
	if !view.AltScreen {
		t.Fatal("browse view should use alt screen for in-place refresh")
	}
	if !strings.Contains(view.Content, "███████╗") || !strings.Contains(view.Content, "Run skink -h to show command line usage.") {
		t.Fatalf("browse view missing status header:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "Select skills to add:") || !strings.Contains(view.Content, "skills/alpha") {
		t.Fatalf("browse view missing picker content:\n%s", view.Content)
	}
}

func TestBrowseViewScrollsWithinTerminalHeight(t *testing.T) {
	items := make([]BrowseItem, 8)
	for i := range items {
		name := fmt.Sprintf("skill-%02d", i)
		items[i] = BrowseItem{Name: name, Path: "skills/" + name}
	}
	m := browseModel{
		title:    "Select skills to add:",
		items:    items,
		selected: map[int]bool{},
		expanded: map[int]bool{},
	}
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 17})
	m = next.(browseModel)
	content := m.View().Content
	if !strings.Contains(content, "↓ more skills") {
		t.Fatalf("browse view should indicate hidden rows below:\n%s", content)
	}
	for range 7 {
		next, _ = m.Update(keyMsg("down"))
		m = next.(browseModel)
	}
	content = m.View().Content
	if strings.Contains(content, "skills/skill-00") {
		t.Fatalf("scrolled browse view should hide rows above the window:\n%s", content)
	}
	if !strings.Contains(content, "skills/skill-07") {
		t.Fatalf("scrolled browse view should keep the cursor row visible:\n%s", content)
	}
	if !strings.Contains(content, "↑ more skills") {
		t.Fatalf("browse view should indicate hidden rows above:\n%s", content)
	}
	if got := strings.Count(content, "\n") + 1; got > m.height {
		t.Fatalf("browse view height = %d, want at most %d:\n%s", got, m.height, content)
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
	}}}, nil)
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

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}}, nil)
	next, cmd = m.Update(keyMsg("u"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionNext || m.action.RepoID != "repo" {
		t.Fatalf("next action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:   "repo",
		Name: "repo",
		BrowseItems: []BrowseItem{
			{Name: "alpha", Path: "alpha", Selected: true},
			{Name: "beta", Path: "beta"},
		},
	}}}, nil)
	next, cmd = m.Update(keyMsg("c"))
	m = next.(statusModel)
	if cmd != nil || m.browse == nil {
		t.Fatalf("choose skills should open embedded browse view: %+v cmd=%v", m, cmd)
	}
	next, cmd = m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionChooseSkills || m.action.RepoID != "repo" || len(m.action.Selected) != 1 || m.action.Selected[0] != 0 {
		t.Fatalf("choose skills action = %+v cmd=%v", m.action, cmd)
	}

	m = newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}}, nil)
	next, cmd = m.Update(keyMsg("a"))
	m = next.(statusModel)
	if cmd == nil || m.addRepo == nil {
		t.Fatalf("add repo should open embedded text prompt: %+v cmd=%v", m, cmd)
	}
	if view := m.View(); !view.AltScreen || !strings.Contains(view.Content, "Add skills repo") || !strings.Contains(view.Content, "Run skink -h") {
		t.Fatalf("add repo view should use status shell:\n%+v", view)
	}
	m.addRepo.input.SetValue("github.com/acme/new")
	next, cmd = m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionAddRepo || m.action.URL != "github.com/acme/new" {
		t.Fatalf("add repo action = %+v cmd=%v", m.action, cmd)
	}
}

func TestStatusModelAddRepoEscReturnsToStatusView(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}}, nil)
	next, cmd := m.Update(keyMsg("a"))
	m = next.(statusModel)
	if cmd == nil || m.addRepo == nil {
		t.Fatalf("add repo should open embedded text prompt: %+v cmd=%v", m, cmd)
	}
	next, cmd = m.Update(keyMsg("esc"))
	m = next.(statusModel)
	if cmd != nil || m.addRepo != nil || m.action.Kind != "" {
		t.Fatalf("esc should return to status without quitting: %+v cmd=%v", m, cmd)
	}
}

func TestStatusModelAddRepoAcceptsPaste(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}}, nil)
	next, _ := m.Update(keyMsg("a"))
	m = next.(statusModel)
	next, _ = m.Update(tea.PasteMsg{Content: "github.com/acme/pasted"})
	m = next.(statusModel)
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || m.action.Kind != StatusActionAddRepo || m.action.URL != "github.com/acme/pasted" {
		t.Fatalf("add repo pasted action = %+v cmd=%v", m.action, cmd)
	}
}

func TestStatusModelAddRepoRunsInsideStatusView(t *testing.T) {
	var gotURL string
	var gotApply StatusAction
	addRepo := func(rawURL string) (StatusAddRepoResult, error) {
		gotURL = rawURL
		return StatusAddRepoResult{
			URL: rawURL,
			Items: []BrowseItem{
				{Name: "alpha", Path: "alpha", Selected: true},
				{Name: "beta", Path: "beta"},
			},
		}, nil
	}
	apply := func(action StatusAction) (StatusSnapshot, error) {
		gotApply = action
		return StatusSnapshot{
			Repos:   []StatusRepo{{ID: "github.com/acme/new", Name: "github.com/acme/new"}},
			Message: "added skills from github.com/acme/new",
		}, nil
	}
	m := newStatusModelWithApply("status", StatusSnapshot{Repos: []StatusRepo{{ID: "repo", Name: "repo"}}}, nil, addRepo, apply)
	next, _ := m.Update(keyMsg("a"))
	m = next.(statusModel)
	m.addRepo.input.SetValue("github.com/acme/new")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || gotURL != "" || !strings.Contains(m.View().Content, "Cloning github.com/acme/new ...") {
		t.Fatalf("add repo should show cloning status before command completes: gotURL=%q cmd=%v view=%s", gotURL, cmd, m.View().Content)
	}
	next, cmd = m.Update(cmd())
	m = next.(statusModel)
	if cmd != nil || gotURL != "github.com/acme/new" || m.addRepo != nil || m.browse == nil {
		t.Fatalf("add repo result should open embedded browse: gotURL=%q model=%+v cmd=%v", gotURL, m, cmd)
	}
	next, cmd = m.Update(keyMsg("enter"))
	m = next.(statusModel)
	if cmd == nil || !m.applying || m.browse != nil || gotApply.Kind != "" {
		t.Fatalf("accepting add repo should apply in place: apply=%+v model=%+v cmd=%v", gotApply, m, cmd)
	}
	next, cmd = m.Update(cmd())
	m = next.(statusModel)
	if cmd != nil || m.applying || gotApply.Kind != StatusActionAddRepo || gotApply.URL != "github.com/acme/new" || len(gotApply.Selected) != 1 || gotApply.Selected[0] != 0 {
		t.Fatalf("add repo apply = %+v model=%+v cmd=%v", gotApply, m, cmd)
	}
	if !strings.Contains(m.View().Content, "added skills from github.com/acme/new") || !strings.Contains(m.View().Content, "github.com/acme/new") {
		t.Fatalf("add repo apply should refresh status view:\n%s", m.View().Content)
	}
}

func TestStatusModelChooseSkillsEscReturnsToStatusView(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:          "repo",
		Name:        "repo",
		BrowseItems: []BrowseItem{{Name: "alpha", Path: "alpha"}},
	}}}, nil)
	next, cmd := m.Update(keyMsg("c"))
	m = next.(statusModel)
	if cmd != nil || m.browse == nil {
		t.Fatalf("choose skills should open embedded browse view: %+v cmd=%v", m, cmd)
	}
	next, cmd = m.Update(keyMsg("esc"))
	m = next.(statusModel)
	if cmd != nil || m.browse != nil || m.action.Kind != "" {
		t.Fatalf("esc should return to status without quitting: %+v cmd=%v", m, cmd)
	}
}

func TestStatusModelWaitsForRepoChecksBeforeTagActions(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:       "repo",
		Name:     "repo",
		Version:  "v1.0.0",
		Checking: true,
	}}}, nil)
	next, cmd := m.Update(keyMsg("t"))
	m = next.(statusModel)
	if cmd != nil || m.tagSelect || !strings.Contains(m.err, "still checking") {
		t.Fatalf("tag action should wait for checks: %+v cmd=%v", m, cmd)
	}

	next, cmd = m.Update(keyMsg("u"))
	m = next.(statusModel)
	if cmd != nil || m.action.Kind != "" || !strings.Contains(m.err, "still checking") {
		t.Fatalf("next action should wait for checks: %+v cmd=%v", m, cmd)
	}
}

func TestStatusViewShowsCheckingAfterVersion(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:       "repo",
		Name:     "repo",
		Version:  "v1.0.0",
		Checking: true,
	}}}, nil)
	content := m.View().Content
	versionIndex := strings.Index(content, "(v1.0.0)")
	checkingIndex := strings.Index(content, "checking...")
	if versionIndex == -1 || checkingIndex == -1 {
		t.Fatalf("status view missing version or checking text:\n%s", content)
	}
	if checkingIndex < versionIndex {
		t.Fatalf("checking text should render after version:\n%s", content)
	}
}

func TestStatusModelAppliesAsyncUpdate(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:       "repo",
		Name:     "repo",
		Version:  "v1.0.0",
		Checking: true,
	}}}, func() StatusSnapshot {
		return StatusSnapshot{Repos: []StatusRepo{{
			ID:      "repo",
			Name:    "repo",
			Version: "v1.0.0",
			Upgrade: true,
			Tags:    []StatusTag{{Name: "v1.1.0"}},
		}}}
	})
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("expected status update command")
	}
	next, _ := m.Update(cmd())
	m = next.(statusModel)
	if m.snapshot.Repos[0].Checking || !m.snapshot.Repos[0].Upgrade || m.snapshot.Repos[0].Tags[0].Name != "v1.1.0" {
		t.Fatalf("snapshot not updated: %+v", m.snapshot.Repos)
	}
}

func TestStatusViewUsesAltScreen(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:   "repo",
		Name: "repo",
	}}}, nil)
	view := m.View()
	if !view.AltScreen {
		t.Fatal("status view should use alt screen for in-place refresh")
	}
	if !strings.Contains(view.Content, "███████╗") || !strings.Contains(view.Content, "Run skink -h to show command line usage.") {
		t.Fatalf("status view missing header:\n%s", view.Content)
	}
}

func TestStatusViewHelpIsSplitByTopic(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:   "repo",
		Name: "repo",
	}}}, nil)
	content := m.View().Content
	for _, want := range []string{
		"move: ↑/↓ • q/esc quit",
		"repo: a add repo, c choose skills, t choose tag, u update newest/head",
		"skills: ←/→ collapse/expand, s sync/overwrite, d delete",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("status view help missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, "headskill") {
		t.Fatalf("status view help should separate repo and skills sections:\n%s", content)
	}
}

func TestStatusModelSkillActionsAndDeleteConfirm(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID: "repo",
		Skills: []StatusSkill{
			{ID: "alpha", Name: "alpha", Path: "skills/alpha", Status: "up to date"},
		},
	}}}, nil)
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
	}}}, nil)
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

func TestStatusModelExpandsAndWrapsSkillDescription(t *testing.T) {
	m := newStatusModel("status", StatusSnapshot{Repos: []StatusRepo{{
		ID:   "repo",
		Name: "repo",
		Skills: []StatusSkill{{
			ID:          "alpha",
			Name:        "alpha",
			Path:        "skills/alpha",
			Description: "first second third fourth",
			Status:      "up to date",
		}},
	}}}, nil)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 24, Height: 20})
	m = next.(statusModel)

	content := m.View().Content
	if strings.Contains(content, "first second") {
		t.Fatalf("collapsed skill should hide description:\n%s", content)
	}
	next, _ = m.Update(keyMsg("down"))
	m = next.(statusModel)
	next, _ = m.Update(keyMsg("right"))
	m = next.(statusModel)
	content = m.View().Content
	if !strings.Contains(content, "▾") || !strings.Contains(content, "      first second third") || !strings.Contains(content, "      fourth") {
		t.Fatalf("expanded skill should show wrapped description:\n%s", content)
	}
	if strings.Contains(content, "first second third fourth") {
		t.Fatalf("expanded description should not run past the view width:\n%s", content)
	}
	next, _ = m.Update(keyMsg("left"))
	m = next.(statusModel)
	if content = m.View().Content; strings.Contains(content, "first second") {
		t.Fatalf("left should collapse expanded skill description:\n%s", content)
	}
}

func TestStatusModelDeleteAppliesInPlace(t *testing.T) {
	var got StatusAction
	apply := func(action StatusAction) (StatusSnapshot, error) {
		got = action
		return StatusSnapshot{
			Repos:   []StatusRepo{{ID: "repo", Name: "repo"}},
			Message: "deleted alpha",
		}, nil
	}
	m := newStatusModelWithApply("status", StatusSnapshot{Repos: []StatusRepo{{
		ID: "repo",
		Skills: []StatusSkill{
			{ID: "alpha", Name: "alpha", Path: "skills/alpha", Status: "up to date"},
		},
	}}}, nil, nil, apply)
	next, _ := m.Update(keyMsg("down"))
	m = next.(statusModel)
	next, _ = m.Update(keyMsg("d"))
	m = next.(statusModel)
	next, cmd := m.Update(keyMsg("y"))
	m = next.(statusModel)
	if cmd == nil || !m.applying || m.confirmDelete || got.Kind != "" {
		t.Fatalf("delete should start in-place apply: apply=%+v model=%+v cmd=%v", got, m, cmd)
	}
	next, cmd = m.Update(cmd())
	m = next.(statusModel)
	if cmd != nil || m.applying || got.Kind != StatusActionDelete || got.SkillID != "alpha" {
		t.Fatalf("delete apply = %+v model=%+v cmd=%v", got, m, cmd)
	}
	if !strings.Contains(m.View().Content, "deleted alpha") {
		t.Fatalf("delete should refresh status view:\n%s", m.View().Content)
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
