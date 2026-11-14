//! Executing a workflow with live progress: `mhl run --format jsonl` writes
//! one JSON event per line (run_start, step_start, step_end, log, result);
//! each is forwarded to the webview over a Tauri channel as it arrives.
//! Anything that is not an event (stderr, a stray stdout line) is wrapped as
//! `{"type":"stderr"|"stdout","text":...}`, and the process exit as
//! `{"type":"exit","code":...}` — always the last message of a run.

use std::collections::HashMap;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::{Arc, Mutex};

use serde::Deserialize;
use serde_json::{json, Value};
use tauri::ipc::Channel;
use tauri::State;
use tokio::io::{AsyncBufReadExt, BufReader};

use crate::sidecar;

#[derive(Default)]
pub struct Runs {
    next: AtomicU64,
    /// run id -> OS process id of its `mhl run`
    pids: Arc<Mutex<HashMap<u64, u32>>>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct RunRequest {
    /// Working directory: the workspace root, where `.mhl/` state lives.
    pub cwd: String,
    pub path: String,
    /// The declared pipeline/workflow to run; None runs the file's first.
    pub workflow: Option<String>,
    #[serde(default)]
    pub inputs: Vec<(String, String)>,
    #[serde(default)]
    pub resume: bool,
    #[serde(default)]
    pub force: bool,
    pub session: Option<String>,
}

/// The `mhl` argument list for a run request.
pub fn run_args(req: &RunRequest) -> Vec<String> {
    let mut args = vec![
        "run".to_string(),
        req.path.clone(),
        "--format".into(),
        "jsonl".into(),
    ];
    if let Some(w) = req.workflow.as_deref().filter(|w| !w.is_empty()) {
        args.push("--workflow".into());
        args.push(w.to_string());
    }
    if req.resume {
        args.push("--resume".into());
        if req.force {
            args.push("--force".into());
        }
    }
    if let Some(s) = req.session.as_deref().filter(|s| !s.is_empty()) {
        args.push("--session".into());
        args.push(s.to_string());
    }
    for (k, v) in &req.inputs {
        args.push("--input".into());
        args.push(format!("{k}={v}"));
    }
    args
}

/// Turns one stdout line into the message sent to the UI.
pub fn stdout_event(line: &str) -> Value {
    match serde_json::from_str::<Value>(line) {
        Ok(v) if v.get("type").and_then(Value::as_str).is_some() => v,
        _ => json!({ "type": "stdout", "text": line }),
    }
}

/// Starts a run and returns its id; events stream on `on_event`.
#[tauri::command]
pub async fn run_start(
    runs: State<'_, Runs>,
    request: RunRequest,
    on_event: Channel<Value>,
) -> Result<u64, String> {
    let mut cmd = sidecar::command(run_args(&request))?;
    cmd.current_dir(&request.cwd);
    let mut child = cmd.spawn().map_err(|e| format!("starting mhl run: {e}"))?;
    let id = runs.next.fetch_add(1, Ordering::Relaxed) + 1;
    if let Some(pid) = child.id() {
        runs.pids.lock().unwrap().insert(id, pid);
    }
    let stdout = child.stdout.take().ok_or("mhl run: no stdout")?;
    let stderr = child.stderr.take().ok_or("mhl run: no stderr")?;

    let err_channel = on_event.clone();
    let err_task = tauri::async_runtime::spawn(async move {
        let mut lines = BufReader::new(stderr).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let _ = err_channel.send(json!({ "type": "stderr", "text": line }));
        }
    });

    let pids = Arc::clone(&runs.pids);
    tauri::async_runtime::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let _ = on_event.send(stdout_event(&line));
        }
        let _ = err_task.await;
        let code = child.wait().await.ok().and_then(|s| s.code());
        pids.lock().unwrap().remove(&id);
        let _ = on_event.send(json!({ "type": "exit", "code": code }));
    });
    Ok(id)
}

/// Cancels a run. Gracefully (SIGINT: mhl cancels at the step boundary and
/// saves a resumable checkpoint) unless `force`, which kills it outright.
#[tauri::command]
pub fn run_cancel(runs: State<'_, Runs>, id: u64, force: bool) -> Result<(), String> {
    let pid = *runs
        .pids
        .lock()
        .unwrap()
        .get(&id)
        .ok_or_else(|| format!("run {id} is not running"))?;
    signal(pid, force)
}

#[cfg(unix)]
fn signal(pid: u32, force: bool) -> Result<(), String> {
    let sig = if force { libc::SIGKILL } else { libc::SIGINT };
    // SAFETY: kill(2) with a pid we spawned and still track as running.
    if unsafe { libc::kill(pid as libc::pid_t, sig) } == 0 {
        Ok(())
    } else {
        Err(format!(
            "signalling mhl run (pid {pid}): {}",
            std::io::Error::last_os_error()
        ))
    }
}

#[cfg(windows)]
fn signal(pid: u32, _force: bool) -> Result<(), String> {
    // A console-less child cannot receive CTRL_BREAK, so Windows cancels by
    // terminating the process tree; resume re-runs the interrupted step.
    std::process::Command::new("taskkill")
        .args(["/PID", &pid.to_string(), "/T", "/F"])
        .status()
        .map_err(|e| format!("taskkill: {e}"))
        .map(|_| ())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds_run_args() {
        let req = RunRequest {
            cwd: ".".into(),
            path: "w.mh".into(),
            workflow: Some("Review".into()),
            inputs: vec![
                ("kind".into(), "large".into()),
                ("approved".into(), "true".into()),
            ],
            resume: true,
            force: false,
            session: Some("abc".into()),
        };
        assert_eq!(
            run_args(&req),
            [
                "run",
                "w.mh",
                "--format",
                "jsonl",
                "--workflow",
                "Review",
                "--resume",
                "--session",
                "abc",
                "--input",
                "kind=large",
                "--input",
                "approved=true"
            ]
        );
    }

    #[test]
    fn classifies_stdout_lines() {
        assert_eq!(
            stdout_event(r#"{"type":"step_start","step":"A"}"#)["step"],
            "A"
        );
        assert_eq!(stdout_event("plain text")["type"], "stdout");
        assert_eq!(stdout_event(r#"{"no":"type"}"#)["type"], "stdout");
    }
}
