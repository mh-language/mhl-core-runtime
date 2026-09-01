package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitSource is a parsed `mhl extension install` argument that points at a git
// remote rather than a local directory:
//
//	<url>[//<subdir>][#<ref>]
//
// e.g. https://github.com/acme/mhl-crm.git
//
//	git@github.com:acme/mhl-crm.git#v1.2.0
//	https://github.com/acme/monorepo.git//ext/crm#main
//
// URL is what gets handed to `git clone`; Subdir (slash-separated, may be "")
// selects a directory inside the clone; Ref (may be "") is a branch, tag, or
// commit. Raw is the whole spec as given, recorded verbatim in the lock.
type gitSource struct {
	URL    string
	Subdir string
	Ref    string
	Raw    string
}

// gitURLSchemes are the transport prefixes we treat as "this is a git remote,
// not a local path".
var gitURLSchemes = []string{"https://", "http://", "ssh://", "git://", "file://", "git+ssh://", "git+https://"}

// parseGitSource splits spec into its url / subdir / ref parts and reports
// whether it looks like a git remote at all. It never touches the network or
// the filesystem — detection is purely lexical, so a real local directory
// (checked by the caller first) always wins over a same-named URL guess.
func parseGitSource(spec string) (gitSource, bool) {
	gs := gitSource{Raw: spec}
	s := spec

	// #ref comes off the end first, so a ref like "release/1.0" keeps its slash.
	if i := strings.LastIndex(s, "#"); i >= 0 {
		gs.Ref = s[i+1:]
		s = s[:i]
	}

	// //subdir separates the clone URL from a path inside it. Skip the
	// scheme's own "//" (as in "https://") before looking.
	searchFrom := 0
	if i := strings.Index(s, "://"); i >= 0 {
		searchFrom = i + 3
	}
	if i := strings.Index(s[searchFrom:], "//"); i >= 0 {
		at := searchFrom + i
		gs.Subdir = strings.Trim(s[at+2:], "/")
		s = s[:at]
	}
	gs.URL = strings.TrimRight(s, "/")

	// git+ssh:// / git+https:// are pip-isms; git itself wants the bare scheme.
	gs.URL = strings.TrimPrefix(gs.URL, "git+")

	return gs, looksLikeGitURL(gs.URL)
}

// looksLikeGitURL is true for an explicit transport scheme, an scp-style
// "user@host:path", or any spec ending in ".git".
func looksLikeGitURL(u string) bool {
	if u == "" {
		return false
	}
	for _, sch := range gitURLSchemes {
		if strings.HasPrefix(u, sch) {
			return true
		}
	}
	if strings.HasSuffix(u, ".git") {
		return true
	}
	// scp-like: "git@github.com:acme/repo" — an '@' and a ':' before any '/'.
	if at := strings.Index(u, "@"); at > 0 {
		rest := u[at+1:]
		if colon := strings.Index(rest, ":"); colon > 0 {
			if slash := strings.Index(rest, "/"); slash < 0 || colon < slash {
				return true
			}
		}
	}
	return false
}

// fetchGitSource clones gs into a fresh temp directory, checks out gs.Ref, and
// returns the directory to install from (the clone, or a subdir of it), the
// 40-hex commit it resolved to, and a cleanup func the caller must defer.
func fetchGitSource(ctx context.Context, gs gitSource, out io.Writer) (dir, commit string, cleanup func(), err error) {
	if _, err = exec.LookPath("git"); err != nil {
		return "", "", func() {}, fmt.Errorf("installing from a git remote needs `git` on PATH: %w", err)
	}

	tmp, err := os.MkdirTemp("", "mhl-ext-git-")
	if err != nil {
		return "", "", func() {}, err
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	clone := filepath.Join(tmp, "repo")

	fmt.Fprintf(out, "cloning %s", gs.URL)
	if gs.Ref != "" {
		fmt.Fprintf(out, " @ %s", gs.Ref)
	}
	fmt.Fprintln(out)

	// A shallow clone straight at the ref covers branches and tags. If git
	// rejects that (a bare commit sha can't be a --branch), fall back to a
	// full clone plus checkout.
	quiet := []string{"-c", "advice.detachedHead=false"}
	shallow := append(append([]string{}, quiet...), "clone", "--quiet", "--depth", "1")
	if gs.Ref != "" {
		shallow = append(shallow, "--branch", gs.Ref)
	}
	shallow = append(shallow, gs.URL, clone)
	if e := runGitCLI(ctx, shallow...); e != nil {
		if gs.Ref == "" {
			cleanup()
			return "", "", func() {}, fmt.Errorf("git clone %s: %w", gs.URL, e)
		}
		if e := runGitCLI(ctx, append(append([]string{}, quiet...), "clone", "--quiet", gs.URL, clone)...); e != nil {
			cleanup()
			return "", "", func() {}, fmt.Errorf("git clone %s: %w", gs.URL, e)
		}
		if e := runGitCLI(ctx, append(append([]string{}, quiet...), "-C", clone, "checkout", "--detach", gs.Ref)...); e != nil {
			cleanup()
			return "", "", func() {}, fmt.Errorf("git checkout %s: %w", gs.Ref, e)
		}
	}

	head, e := captureGitCLI(ctx, "-C", clone, "rev-parse", "HEAD")
	if e != nil {
		cleanup()
		return "", "", func() {}, fmt.Errorf("resolving HEAD: %w", e)
	}
	commit = strings.TrimSpace(head)

	dir = clone
	if gs.Subdir != "" {
		sub := filepath.Join(clone, filepath.FromSlash(gs.Subdir))
		rel, e := filepath.Rel(clone, sub)
		if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			cleanup()
			return "", "", func() {}, fmt.Errorf("subdir %q escapes the repository", gs.Subdir)
		}
		if fi, e := os.Stat(sub); e != nil || !fi.IsDir() {
			cleanup()
			return "", "", func() {}, fmt.Errorf("subdir %q not found in %s", gs.Subdir, gs.URL)
		}
		dir = sub
	}
	return dir, commit, cleanup, nil
}

// runGitCLI runs a git command, letting its stderr through to the user's
// terminal (clone progress, auth failures) and failing closed. Interactive
// credential prompts are disabled so a private repo without ambient auth
// errors out instead of blocking.
func runGitCLI(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func captureGitCLI(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	b, err := cmd.Output()
	return string(b), err
}
