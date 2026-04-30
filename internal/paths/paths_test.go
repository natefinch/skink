package paths

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	env := FakeEnv{Home: "/u/me"}
	l, err := Resolve(env)
	if err != nil {
		t.Fatal(err)
	}
	if l.Home != "/u/me" {
		t.Errorf("home = %q", l.Home)
	}
	if l.SkinkHome != filepath.Join("/u/me", ".skink") {
		t.Errorf("skink home = %q", l.SkinkHome)
	}
}

func TestResolveErrors(t *testing.T) {
	if _, err := Resolve(nil); err == nil {
		t.Error("nil env should error")
	}
	if _, err := Resolve(FakeEnv{HomeErr: errors.New("boom")}); err == nil {
		t.Error("home err should propagate")
	}
	if _, err := Resolve(FakeEnv{Home: ""}); err == nil {
		t.Error("empty home should error")
	}
}

func TestProjectRoot(t *testing.T) {
	got, err := ProjectRoot(FakeEnv{Wd: "/proj"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "/proj" {
		t.Errorf("wd = %q", got)
	}

	if _, err := ProjectRoot(nil); err == nil {
		t.Error("nil env should error")
	}
	if _, err := ProjectRoot(FakeEnv{WdErr: errors.New("x")}); err == nil {
		t.Error("wd err should propagate")
	}
	if _, err := ProjectRoot(FakeEnv{Wd: ""}); err == nil {
		t.Error("empty wd should error")
	}
}
