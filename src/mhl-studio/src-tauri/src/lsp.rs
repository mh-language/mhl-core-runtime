//! Bridge between the webview's LSP client and `mhl lsp` (stdio,
//! Content-Length framed JSON-RPC). Messages from the server are emitted as
//! `lsp://message` events (the raw JSON text); the client sends with
//! `lsp_send`. One server per studio window, restarted when a folder is opened.

use std::process::Stdio;

use tauri::{AppHandle, Emitter, State};
use tokio::io::{
    AsyncBufRead, AsyncBufReadExt, AsyncReadExt, AsyncWrite, AsyncWriteExt, BufReader,
};
use tokio::process::{Child, ChildStdin};
use tokio::sync::Mutex;

use crate::sidecar;

#[derive(Default)]
pub struct Lsp(Mutex<Option<Server>>);

pub struct Server {
    stdin: ChildStdin,
    _child: Child, // kill_on_drop: replacing the Server stops the old process
}

/// Reads one Content-Length framed message; Ok(None) at a clean EOF.
pub async fn read_frame<R: AsyncBufRead + Unpin>(r: &mut R) -> std::io::Result<Option<String>> {
    let mut len: Option<usize> = None;
    loop {
        let mut line = String::new();
        if r.read_line(&mut line).await? == 0 {
            return Ok(None);
        }
        let line = line.trim_end_matches(['\r', '\n']);
        if line.is_empty() {
            if len.is_some() {
                break;
            }
            continue; // stray blank line between frames
        }
        if let Some((name, value)) = line.split_once(':') {
            if name.trim().eq_ignore_ascii_case("content-length") {
                len = value.trim().parse().ok();
            }
        }
    }
    let mut body = vec![0u8; len.unwrap_or(0)];
    r.read_exact(&mut body).await?;
    Ok(Some(String::from_utf8_lossy(&body).into_owned()))
}

/// Writes one framed message.
pub async fn write_frame<W: AsyncWrite + Unpin>(w: &mut W, message: &str) -> std::io::Result<()> {
    let header = format!("Content-Length: {}\r\n\r\n", message.len());
    w.write_all(header.as_bytes()).await?;
    w.write_all(message.as_bytes()).await?;
    w.flush().await
}

/// (Re)starts `mhl lsp` in `cwd`. The previous server, if any, is stopped.
#[tauri::command]
pub async fn lsp_start(app: AppHandle, lsp: State<'_, Lsp>, cwd: String) -> Result<(), String> {
    let mut cmd = sidecar::command(["lsp"])?;
    cmd.current_dir(&cwd).stderr(Stdio::null());
    let mut child = cmd.spawn().map_err(|e| format!("starting mhl lsp: {e}"))?;
    let stdin = child.stdin.take().ok_or("mhl lsp: no stdin")?;
    let stdout = child.stdout.take().ok_or("mhl lsp: no stdout")?;
    *lsp.0.lock().await = Some(Server {
        stdin,
        _child: child,
    });

    tauri::async_runtime::spawn(async move {
        let mut reader = BufReader::new(stdout);
        while let Ok(Some(msg)) = read_frame(&mut reader).await {
            let _ = app.emit("lsp://message", msg);
        }
        let _ = app.emit("lsp://exit", ());
    });
    Ok(())
}

#[tauri::command]
pub async fn lsp_send(lsp: State<'_, Lsp>, message: String) -> Result<(), String> {
    let mut guard = lsp.0.lock().await;
    let server = guard.as_mut().ok_or("the language server is not running")?;
    write_frame(&mut server.stdin, &message)
        .await
        .map_err(|e| format!("writing to mhl lsp: {e}"))
}

#[tauri::command]
pub async fn lsp_stop(lsp: State<'_, Lsp>) -> Result<(), String> {
    lsp.0.lock().await.take();
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn frames_round_trip() {
        let mut buf: Vec<u8> = Vec::new();
        write_frame(&mut buf, r#"{"id":1,"result":"é"}"#)
            .await
            .unwrap();
        write_frame(&mut buf, r#"{"method":"x"}"#).await.unwrap();
        let mut r = BufReader::new(&buf[..]);
        assert_eq!(
            read_frame(&mut r).await.unwrap().unwrap(),
            r#"{"id":1,"result":"é"}"#
        );
        assert_eq!(
            read_frame(&mut r).await.unwrap().unwrap(),
            r#"{"method":"x"}"#
        );
        assert_eq!(read_frame(&mut r).await.unwrap(), None);
    }

    #[tokio::test]
    async fn reads_extra_headers() {
        let raw = "Content-Type: application/vscode-jsonrpc\r\ncontent-length: 2\r\n\r\n{}";
        let mut r = BufReader::new(raw.as_bytes());
        assert_eq!(read_frame(&mut r).await.unwrap().unwrap(), "{}");
    }

    /// Talks to the real vendored `mhl lsp`: initialize must answer.
    #[tokio::test]
    async fn initializes_the_vendored_server() {
        let bin = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("binaries");
        let Some(mhl) = std::fs::read_dir(&bin)
            .ok()
            .and_then(|mut d| d.find_map(|e| e.ok().map(|e| e.path())))
        else {
            eprintln!(
                "skipping: no sidecar in {} (run `npm run sidecar`)",
                bin.display()
            );
            return;
        };
        let mut child = tokio::process::Command::new(mhl)
            .arg("lsp")
            .stdin(Stdio::piped())
            .stdout(Stdio::piped())
            .kill_on_drop(true)
            .spawn()
            .unwrap();
        let mut stdin = child.stdin.take().unwrap();
        let mut out = BufReader::new(child.stdout.take().unwrap());
        write_frame(
            &mut stdin,
            r#"{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"capabilities":{}}}"#,
        )
        .await
        .unwrap();
        let reply = read_frame(&mut out).await.unwrap().unwrap();
        let v: serde_json::Value = serde_json::from_str(&reply).unwrap();
        assert_eq!(v["id"], 1);
        assert!(v["result"]["capabilities"]["completionProvider"].is_object());
    }
}
