//! Locating and spawning the vendored `mhl` binary.
//!
//! Tauri copies `bundle.externalBin` entries (`binaries/mhl-<triple>`) next
//! to the app executable as plain `mhl` / `mhl.exe` — in `tauri dev`
//! (target/debug) and in a bundle alike. The studio always runs that copy,
//! never an `mhl` on PATH, so the language it edits is exactly the runtime
//! it ships with. `MHL_STUDIO_SIDECAR` overrides the path (tests, debugging).

use std::path::PathBuf;
use std::process::Stdio;

use tokio::process::Command;

pub fn mhl_path() -> Result<PathBuf, String> {
    if let Some(p) = std::env::var_os("MHL_STUDIO_SIDECAR") {
        return Ok(PathBuf::from(p));
    }
    let exe = std::env::current_exe().map_err(|e| format!("locating the studio executable: {e}"))?;
    let dir = exe
        .parent()
        .ok_or_else(|| "the studio executable has no parent directory".to_string())?;
    let name = if cfg!(windows) { "mhl.exe" } else { "mhl" };
    let path = dir.join(name);
    if path.exists() {
        Ok(path)
    } else {
        Err(format!(
            "the bundled mhl runtime was not found at {} (run `npm run sidecar`)",
            path.display()
        ))
    }
}

/// A `mhl <args>` command with piped stdio, killed if its handle is dropped.
pub fn command<I, S>(args: I) -> Result<Command, String>
where
    I: IntoIterator<Item = S>,
    S: AsRef<std::ffi::OsStr>,
{
    let mut cmd = Command::new(mhl_path()?);
    cmd.args(args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(true);
    #[cfg(windows)]
    {
        // CREATE_NO_WINDOW | CREATE_NEW_PROCESS_GROUP: no console flash, and
        // a group of its own so a cancel can target just this run.
        cmd.creation_flags(0x0800_0000 | 0x0000_0200);
    }
    Ok(cmd)
}
