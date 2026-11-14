//! One-shot `mhl` invocations that return a JSON document: the workflow
//! graph of an editor buffer (`mhl graph --stdin`) and the static plan of a
//! run (`mhl run --dry-run --format json`).

use std::path::Path;

use serde_json::Value;
use tokio::io::AsyncWriteExt;

use crate::sidecar;

#[tauri::command]
pub async fn mhl_version() -> Result<String, String> {
    let out = sidecar::command(["version"])?
        .output()
        .await
        .map_err(|e| format!("running mhl version: {e}"))?;
    Ok(String::from_utf8_lossy(&out.stdout).trim().to_string())
}

/// The control-flow graph of `source`, an editor buffer for the file at
/// `path` (which need not be saved; imports resolve relative to it).
#[tauri::command]
pub async fn graph(path: String, source: String) -> Result<Value, String> {
    let mut cmd = sidecar::command(["graph", path.as_str(), "--stdin"])?;
    if let Some(dir) = Path::new(&path).parent().filter(|d| d.is_dir()) {
        cmd.current_dir(dir);
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| format!("starting mhl graph: {e}"))?;
    let mut stdin = child.stdin.take().ok_or("mhl graph: no stdin")?;
    stdin
        .write_all(source.as_bytes())
        .await
        .map_err(|e| format!("mhl graph: writing source: {e}"))?;
    drop(stdin);
    let out = child
        .wait_with_output()
        .await
        .map_err(|e| format!("mhl graph: {e}"))?;
    if !out.status.success() {
        return Err(format!(
            "mhl graph failed: {}",
            String::from_utf8_lossy(&out.stderr).trim()
        ));
    }
    serde_json::from_slice(&out.stdout).map_err(|e| format!("mhl graph: bad JSON: {e}"))
}

/// The static plan `mhl run --dry-run --format json` reports for the saved
/// file at `path`, run from `cwd` with `inputs` — declared inputs with their
/// types, missing/invalid ones, lint warnings. A non-zero exit (problems
/// found) still carries the JSON document, so it is returned, not an error.
#[tauri::command]
pub async fn inspect(
    cwd: String,
    path: String,
    workflow: Option<String>,
    inputs: Vec<(String, String)>,
) -> Result<Value, String> {
    let mut args = vec![
        "run".to_string(),
        path,
        "--dry-run".into(),
        "--format".into(),
        "json".into(),
    ];
    if let Some(w) = workflow.filter(|w| !w.is_empty()) {
        args.push("--workflow".into());
        args.push(w);
    }
    for (k, v) in inputs {
        args.push("--input".into());
        args.push(format!("{k}={v}"));
    }
    let mut cmd = sidecar::command(&args)?;
    cmd.current_dir(&cwd);
    let out = cmd
        .output()
        .await
        .map_err(|e| format!("running mhl run --dry-run: {e}"))?;
    serde_json::from_slice(&out.stdout).map_err(|_| {
        format!(
            "mhl run --dry-run: {}",
            String::from_utf8_lossy(&out.stderr)
                .trim()
                .trim_start_matches("mhl: ")
        )
    })
}

/// Evaluates `expr` — e.g. a tool method call, `Paths.root("a")` — against
/// the declarations of the saved file at `path`, run from `cwd`, and returns
/// `mhl run --format json`'s result document (its `vars.result` holds the
/// value; `error` the failure).
///
/// The language has no "evaluate an expression" command, so this writes a
/// throwaway program that imports the whole file and runs one step:
///
/// ```text
/// import "<path, relative to the temp file>"
/// workflow __mhl_studio_eval { checkpoint: { enabled: false } ... }
/// ```
///
/// in a fresh temp directory (imports only resolve relative paths, hence the
/// relative import), with checkpointing off so nothing is written to `cwd`.
#[tauri::command]
pub async fn evaluate(cwd: String, path: String, expr: String) -> Result<Value, String> {
    let target = std::fs::canonicalize(&path).map_err(|e| format!("{path}: {e}"))?;
    let dir = std::env::temp_dir().join(format!(
        "mhl-studio-eval-{}-{}",
        std::process::id(),
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or_default()
    ));
    std::fs::create_dir_all(&dir).map_err(|e| format!("creating {}: {e}", dir.display()))?;
    let _cleanup = RemoveOnDrop(dir.clone());
    let tmp_dir = std::fs::canonicalize(&dir).map_err(|e| format!("{}: {e}", dir.display()))?;
    // A file on another drive (Windows) has no relative path from the temp
    // dir: evaluate next to it instead.
    let (program, import) = match relative_path(&tmp_dir, &target) {
        Some(rel) => (tmp_dir.join("eval.mh"), rel),
        None => {
            let parent = target.parent().ok_or("file has no parent directory")?;
            let name = target
                .file_name()
                .ok_or("file has no name")?
                .to_string_lossy()
                .to_string();
            (
                parent.join(format!(".mhl-studio-eval-{}.mh", std::process::id())),
                name,
            )
        }
    };
    let _cleanup_program = RemoveOnDrop(program.clone());
    std::fs::write(&program, eval_program(&import, &expr))
        .map_err(|e| format!("writing {}: {e}", program.display()))?;

    let program_arg = program.to_string_lossy().to_string();
    let mut cmd = sidecar::command([
        "run",
        program_arg.as_str(),
        "--workflow",
        EVAL_WORKFLOW,
        "--format",
        "json",
    ])?;
    cmd.current_dir(&cwd);
    let out = tokio::time::timeout(std::time::Duration::from_secs(120), cmd.output())
        .await
        .map_err(|_| "the call did not finish within 2 minutes".to_string())?
        .map_err(|e| format!("running mhl: {e}"))?;
    serde_json::from_slice(&out.stdout).map_err(|_| {
        format!(
            "mhl: {}",
            String::from_utf8_lossy(&out.stderr)
                .trim()
                .trim_start_matches("mhl: ")
        )
    })
}

const EVAL_WORKFLOW: &str = "__mhl_studio_eval";

fn eval_program(import: &str, expr: &str) -> String {
    let path = serde_json::to_string(import).unwrap_or_default();
    format!(
        "import {path}\n\nworkflow {EVAL_WORKFLOW} {{\n    checkpoint: {{ enabled: false }}\n    var result = null\n    step call {{\n        self.result = {expr}\n    }}\n}}\n"
    )
}

/// `to` relative to directory `from`, with `/` separators, or None when they
/// share no root (different Windows drives).
fn relative_path(from: &Path, to: &Path) -> Option<String> {
    use std::path::Component;
    let a: Vec<Component> = from.components().collect();
    let b: Vec<Component> = to.components().collect();
    if a.first() != b.first() {
        return None;
    }
    let common = a.iter().zip(&b).take_while(|(x, y)| x == y).count();
    let mut parts: Vec<String> = vec!["..".into(); a.len() - common];
    parts.extend(
        b[common..]
            .iter()
            .map(|c| c.as_os_str().to_string_lossy().to_string()),
    );
    Some(parts.join("/"))
}

struct RemoveOnDrop(std::path::PathBuf);

impl Drop for RemoveOnDrop {
    fn drop(&mut self) {
        if self.0.is_dir() {
            let _ = std::fs::remove_dir_all(&self.0);
        } else {
            let _ = std::fs::remove_file(&self.0);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn relative_paths() {
        assert_eq!(
            relative_path(Path::new("/tmp/x/eval"), Path::new("/Users/me/flows/a.mh")).as_deref(),
            Some("../../../Users/me/flows/a.mh")
        );
        assert_eq!(
            relative_path(Path::new("/a/b"), Path::new("/a/b/c.mh")).as_deref(),
            Some("c.mh")
        );
    }

    /// Runs `evaluate` against the vendored binary (skipped before `npm run sidecar`).
    #[tokio::test]
    async fn evaluates_a_tool_method_without_touching_the_workspace() {
        let bin = std::fs::read_dir(concat!(env!("CARGO_MANIFEST_DIR"), "/binaries"))
            .ok()
            .and_then(|d| {
                d.flatten().map(|e| e.path()).find(|p| {
                    p.file_name()
                        .is_some_and(|n| n.to_string_lossy().starts_with("mhl-"))
                })
            });
        let Some(bin) = bin else { return };
        if std::env::var_os("MHL_STUDIO_SIDECAR").is_none() {
            // SAFETY: no other test reads or writes the environment concurrently.
            unsafe { std::env::set_var("MHL_STUDIO_SIDECAR", &bin) };
        }
        let ws = std::env::temp_dir().join(format!("mhl-studio-eval-test-{}", std::process::id()));
        std::fs::create_dir_all(&ws).unwrap();
        let file = ws.join("lib.mh");
        std::fs::write(&file, "tool T {\n    twice(x: number, k: number = 2): number -> x * k\n    boom() -> fail(\"nope\")\n}\n").unwrap();
        let (cwd, path) = (
            ws.to_string_lossy().to_string(),
            file.to_string_lossy().to_string(),
        );

        let ok = evaluate(cwd.clone(), path.clone(), "T.twice(21)".into())
            .await
            .unwrap();
        assert_eq!(ok["ok"], true, "{ok}");
        assert_eq!(ok["vars"]["result"], 42);
        let failed = evaluate(cwd, path, "T.boom()".into()).await.unwrap();
        assert_eq!(failed["ok"], false);
        assert!(
            failed["error"].as_str().unwrap().contains("nope"),
            "{failed}"
        );

        let left: Vec<_> = std::fs::read_dir(&ws)
            .unwrap()
            .flatten()
            .map(|e| e.file_name())
            .collect();
        assert_eq!(
            left,
            vec![std::ffi::OsString::from("lib.mh")],
            "evaluate wrote into the workspace"
        );
        std::fs::remove_dir_all(&ws).unwrap();
    }

    #[test]
    fn program_quotes_the_import() {
        let p = eval_program("../dir \"q\"/a.mh", "T.m(1)");
        assert!(p.starts_with("import \"../dir \\\"q\\\"/a.mh\"\n"));
        assert!(p.contains("self.result = T.m(1)"));
        assert!(p.contains("checkpoint: { enabled: false }"));
    }
}
