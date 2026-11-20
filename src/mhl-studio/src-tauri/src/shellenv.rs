//! The user's command-line environment, for the agent CLIs a workflow runs.
//!
//! An app launched from the Finder/Dock (or a desktop launcher) does not
//! inherit a terminal's environment: it gets the bare system PATH
//! (`/usr/bin:/bin:/usr/sbin:/sbin` on macOS), so `claude`, `codex`,
//! `copilot`, `agy` or `devin` — usually in `~/.local/bin` or
//! `/opt/homebrew/bin` — are "not found" when `mhl` spawns them, and none of
//! the variables exported in `.zshrc`/`.zprofile` (`GEMINI_API_KEY`,
//! `OPENAI_API_KEY`, ...) are set. At startup the studio asks the user's login
//! shell for its environment and imports it: PATH is merged (the shell's
//! entries first), any other variable is added only when this process lacks
//! it. Every child (the `mhl` sidecar, the agents it runs) inherits the result.

use std::collections::HashMap;
use std::path::{Path, PathBuf};

const MARK: &str = "__MHL_STUDIO_ENV__";

/// Per-shell or per-terminal variables that say nothing about the user's
/// setup and would mislead the studio's children.
const SKIP: &[&str] = &[
    "_",
    "PWD",
    "OLDPWD",
    "SHLVL",
    "TERM",
    "TERM_PROGRAM",
    "TERM_PROGRAM_VERSION",
    "TERM_SESSION_ID",
    "COLORTERM",
];

/// The `env -0` dump printed between two markers (a login shell may print
/// banners around it), as name/value pairs.
pub fn extract_env(stdout: &str) -> Option<Vec<(String, String)>> {
    let start = stdout.find(MARK)? + MARK.len();
    let len = stdout[start..].rfind(MARK)?;
    let vars: Vec<(String, String)> = stdout[start..start + len]
        .split('\0')
        .filter_map(|entry| {
            let (name, value) = entry.split_once('=')?;
            let name = name.trim_start_matches('\n');
            (!name.is_empty()).then(|| (name.to_string(), value.to_string()))
        })
        .collect();
    (!vars.is_empty()).then_some(vars)
}

/// `shell` entries first, then the `current` ones it lacks — no duplicates.
pub fn merge_paths(shell: &str, current: &str) -> String {
    let mut seen = std::collections::HashSet::new();
    let mut out = Vec::new();
    for dir in shell.split(':').chain(current.split(':')) {
        if !dir.is_empty() && seen.insert(dir.to_string()) {
            out.push(dir.to_string());
        }
    }
    out.join(":")
}

/// The variables to set in this process from the login shell's `shell`
/// environment: PATH merged, the others only when `current` lacks them.
pub fn env_updates(
    shell: &[(String, String)],
    current: impl Fn(&str) -> Option<String>,
) -> Vec<(String, String)> {
    shell
        .iter()
        .filter(|(name, _)| !SKIP.contains(&name.as_str()))
        .filter_map(|(name, value)| match current(name) {
            _ if name == "PATH" => Some((
                name.clone(),
                merge_paths(value, &current("PATH").unwrap_or_default()),
            )),
            Some(_) => None,
            None => Some((name.clone(), value.clone())),
        })
        .collect()
}

/// Imports the login shell's environment into this process. Best effort: a
/// shell that fails or takes over 5 seconds leaves the environment as it was.
#[cfg(unix)]
pub fn import_login_env() {
    use std::io::Read;
    use std::process::{Command, Stdio};
    use std::time::{Duration, Instant};

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());
    // -i as well as -l: zsh users commonly extend PATH and export API keys in
    // .zshrc, which only an interactive shell reads. `env -0` keeps a value
    // holding a newline intact.
    let script = format!("printf '%s' '{MARK}'; /usr/bin/env -0; printf '%s' '{MARK}'");
    let Ok(mut child) = Command::new(&shell)
        .args(["-ilc", &script])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
    else {
        return;
    };
    // Read while waiting: a large environment must not fill the pipe and stall the shell.
    let mut stdout = child.stdout.take();
    let reader = std::thread::spawn(move || {
        let mut out = String::new();
        if let Some(s) = stdout.as_mut() {
            let _ = s.read_to_string(&mut out);
        }
        out
    });
    let deadline = Instant::now() + Duration::from_secs(5);
    loop {
        match child.try_wait() {
            Ok(Some(_)) => break,
            Ok(None) if Instant::now() < deadline => std::thread::sleep(Duration::from_millis(20)),
            _ => {
                let _ = child.kill();
                let _ = child.wait();
                return;
            }
        }
    }
    let Ok(out) = reader.join() else { return };
    let Some(shell_env) = extract_env(&out) else {
        return;
    };
    for (name, value) in env_updates(&shell_env, |n| std::env::var(n).ok()) {
        // SAFETY: called once at startup, before Tauri spawns any thread
        // (the reader thread above has already been joined).
        unsafe { std::env::set_var(name, value) };
    }
}

#[cfg(not(unix))]
pub fn import_login_env() {}

/// The first executable named `name` in the directories of `path_var`.
pub fn find_in_path(name: &str, path_var: &str) -> Option<PathBuf> {
    let exts: Vec<String> = if cfg!(windows) {
        std::env::var("PATHEXT")
            .unwrap_or_else(|_| ".EXE;.CMD;.BAT".into())
            .split(';')
            .map(|e| e.to_lowercase())
            .collect()
    } else {
        vec![String::new()]
    };
    std::env::split_paths(path_var).find_map(|dir| {
        exts.iter()
            .map(|ext| dir.join(format!("{name}{ext}")))
            .find(|p| is_executable(p))
    })
}

fn is_executable(p: &Path) -> bool {
    let Ok(meta) = std::fs::metadata(p) else {
        return false;
    };
    if !meta.is_file() {
        return false;
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        meta.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    true
}

/// Where each of the named CLIs is on PATH (absent ones are left out), so
/// the agent templates can tell which backends this machine can run.
#[tauri::command]
pub fn find_clis(names: Vec<String>) -> HashMap<String, String> {
    let path = std::env::var("PATH").unwrap_or_default();
    names
        .into_iter()
        .filter_map(|n| find_in_path(&n, &path).map(|p| (n, p.to_string_lossy().into_owned())))
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn pairs(v: &[(&str, &str)]) -> Vec<(String, String)> {
        v.iter()
            .map(|(a, b)| (a.to_string(), b.to_string()))
            .collect()
    }

    #[test]
    fn extracts_the_env_between_markers() {
        let out =
            format!("Welcome!\n{MARK}PATH=/a/bin:/usr/bin\0KEY=x=y\0MULTI=one\ntwo\0{MARK}\n");
        assert_eq!(
            extract_env(&out),
            Some(pairs(&[
                ("PATH", "/a/bin:/usr/bin"),
                ("KEY", "x=y"),
                ("MULTI", "one\ntwo")
            ]))
        );
        assert_eq!(extract_env("no markers"), None);
        assert_eq!(extract_env(&format!("{MARK}{MARK}")), None);
    }

    #[test]
    fn adds_missing_vars_merges_path_and_keeps_existing() {
        let shell = pairs(&[
            ("PATH", "/home/u/.local/bin:/usr/bin"),
            ("GEMINI_API_KEY", "from-shell"),
            ("HOME", "/shell/home"),
            ("PWD", "/somewhere"),
            ("SHLVL", "2"),
        ]);
        let current = |n: &str| match n {
            "PATH" => Some("/usr/bin:/bin".to_string()),
            "HOME" => Some("/app/home".to_string()),
            _ => None,
        };
        assert_eq!(
            env_updates(&shell, current),
            pairs(&[
                ("PATH", "/home/u/.local/bin:/usr/bin:/bin"),
                ("GEMINI_API_KEY", "from-shell")
            ])
        );
    }

    #[test]
    fn merges_shell_first_without_duplicates() {
        assert_eq!(
            merge_paths("/home/u/.local/bin:/usr/bin", "/usr/bin:/bin"),
            "/home/u/.local/bin:/usr/bin:/bin"
        );
        assert_eq!(merge_paths("", "/usr/bin"), "/usr/bin");
    }

    #[cfg(unix)]
    #[test]
    fn finds_only_executables_on_path() {
        use std::os::unix::fs::PermissionsExt;
        let dir = std::env::temp_dir().join(format!("mhl-studio-which-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        let exe = dir.join("fakecli");
        std::fs::write(&exe, "#!/bin/sh\n").unwrap();
        std::fs::set_permissions(&exe, std::fs::Permissions::from_mode(0o755)).unwrap();
        std::fs::write(dir.join("plainfile"), "").unwrap();
        let path = format!("/nonexistent:{}", dir.display());
        assert_eq!(find_in_path("fakecli", &path), Some(exe));
        assert_eq!(find_in_path("plainfile", &path), None);
        assert_eq!(find_in_path("missing", &path), None);
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
