//! The user's command-line environment, for the agent CLIs a workflow runs.
//!
//! An app launched from the Finder/Dock (or a desktop launcher) does not
//! inherit the PATH a terminal has: it gets the bare system one
//! (`/usr/bin:/bin:/usr/sbin:/sbin` on macOS), so `claude`, `codex`,
//! `copilot`, `agy` or `devin` — usually in `~/.local/bin` or
//! `/opt/homebrew/bin` — are "not found" when `mhl` spawns them. At startup
//! the studio asks the user's login shell for its PATH and prepends what is
//! missing; every child (the `mhl` sidecar, the agents it runs) inherits it.

use std::collections::HashMap;
use std::path::{Path, PathBuf};

const MARK: &str = "__MHL_STUDIO_PATH__";

/// The PATH printed between two markers (a login shell may print banners).
pub fn extract_path(stdout: &str) -> Option<String> {
    let start = stdout.find(MARK)? + MARK.len();
    let len = stdout[start..].find(MARK)?;
    let path = stdout[start..start + len].trim();
    (!path.is_empty()).then(|| path.to_string())
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

/// Imports the login shell's PATH into this process. Best effort: a shell
/// that fails or takes over 5 seconds leaves PATH as it was.
#[cfg(unix)]
pub fn import_login_path() {
    use std::io::Read;
    use std::process::{Command, Stdio};
    use std::time::{Duration, Instant};

    let shell = std::env::var("SHELL").unwrap_or_else(|_| "/bin/zsh".to_string());
    // -i as well as -l: zsh users commonly extend PATH in .zshrc, which only
    // an interactive shell reads.
    let script = format!("printf '%s%s%s' '{MARK}' \"$PATH\" '{MARK}'");
    let Ok(mut child) = Command::new(&shell)
        .args(["-ilc", &script])
        .stdin(Stdio::null())
        .stdout(Stdio::piped())
        .stderr(Stdio::null())
        .spawn()
    else {
        return;
    };
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
    let mut out = String::new();
    if let Some(mut stdout) = child.stdout.take() {
        let _ = stdout.read_to_string(&mut out);
    }
    if let Some(shell_path) = extract_path(&out) {
        let current = std::env::var("PATH").unwrap_or_default();
        // SAFETY: called once at startup, before Tauri spawns any thread.
        unsafe { std::env::set_var("PATH", merge_paths(&shell_path, &current)) };
    }
}

#[cfg(not(unix))]
pub fn import_login_path() {}

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
    let Ok(meta) = std::fs::metadata(p) else { return false };
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

    #[test]
    fn extracts_the_path_between_markers() {
        let out = format!("Welcome!\n{MARK}/a/bin:/usr/bin{MARK}\n");
        assert_eq!(extract_path(&out).as_deref(), Some("/a/bin:/usr/bin"));
        assert_eq!(extract_path("no markers"), None);
        assert_eq!(extract_path(&format!("{MARK}{MARK}")), None);
    }

    #[test]
    fn merges_shell_first_without_duplicates() {
        assert_eq!(merge_paths("/home/u/.local/bin:/usr/bin", "/usr/bin:/bin"), "/home/u/.local/bin:/usr/bin:/bin");
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
