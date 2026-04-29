// Package paths resolves filesystem locations used by skink:
// the user's home directory, the skink home (~/.skink), the checkout
// directory inside it, and the project root for an install.
//
// The package is pure and FS-free aside from calls through an Env
// abstraction, so tests can inject overrides.
package paths

import (
	"errors"
	"os"
	"path/filepath"
)

// Env abstracts the bits of the OS environment that paths needs. Production
// code uses OSEnv; tests use a struct literal.
type Env interface {
	UserHomeDir() (string, error)
	Getwd() (string, error)
}

// OSEnv is the production Env that delegates to the os package.
type OSEnv struct{}

func (OSEnv) UserHomeDir() (string, error) { return os.UserHomeDir() }
func (OSEnv) Getwd() (string, error)       { return os.Getwd() }

// FakeEnv is a test-friendly Env.
type FakeEnv struct {
	Home    string
	HomeErr error
	Wd      string
	WdErr   error
}

func (f FakeEnv) UserHomeDir() (string, error) {
	if f.HomeErr != nil {
		return "", f.HomeErr
	}
	return f.Home, nil
}
func (f FakeEnv) Getwd() (string, error) {
	if f.WdErr != nil {
		return "", f.WdErr
	}
	return f.Wd, nil
}

// Layout holds the resolved skink paths.
type Layout struct {
	Home      string // user home, e.g. /Users/me
	SkinkHome string // ~/.skink
	Checkout  string // ~/.skink/repo
	Config    string // ~/.skink/config.yaml
}

// Resolve builds a Layout from the given Env.
func Resolve(env Env) (Layout, error) {
	if env == nil {
		return Layout{}, errors.New("paths: nil Env")
	}
	home, err := env.UserHomeDir()
	if err != nil {
		return Layout{}, err
	}
	if home == "" {
		return Layout{}, errors.New("paths: empty home directory")
	}
	sh := filepath.Join(home, ".skink")
	return Layout{
		Home:      home,
		SkinkHome: sh,
		Checkout:  filepath.Join(sh, "repo"),
		Config:    filepath.Join(sh, "config.yaml"),
	}, nil
}

// ProjectRoot returns the working directory. It is a separate call because
// `init` may be run outside of any project, while `install`/`uninstall` need
// a project root.
func ProjectRoot(env Env) (string, error) {
	if env == nil {
		return "", errors.New("paths: nil Env")
	}
	wd, err := env.Getwd()
	if err != nil {
		return "", err
	}
	if wd == "" {
		return "", errors.New("paths: empty working directory")
	}
	return wd, nil
}
