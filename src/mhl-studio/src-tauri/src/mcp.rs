//! The MCP test bench: the open workspace served as an MCP server by the
//! vendored runtime (`mhl serve mcp .`), so the studio can call it the way
//! an MCP client (Claude Desktop, VS Code) would.
//!
//! Two transports, one server at a time:
//! - **stdio** — newline-delimited JSON-RPC. `mcp_send` writes a line to the
//!   server's stdin; each stdout line comes back on the start channel as
//!   `{"type":"message","text":...}`.
//! - **HTTP** — `serve mcp --http` on a free loopback port, with a bearer
//!   token the UI generates (passed in the environment, never argv).
//!   `mcp_http` makes one request and returns the reply. It goes through
//!   here rather than the webview's `fetch`: the server refuses a
//!   non-loopback `Origin`, and a `tauri://` page always sends one.
//!
//! The server's diagnostics (stderr, and stdout in HTTP mode) arrive as
//! `{"type":"log","text":...}`, and `{"type":"exit"}` is the channel's last
//! message, once the process has closed its output.

use std::io::{Read, Write};
use std::net::{Ipv4Addr, TcpListener, TcpStream};
use std::time::{Duration, Instant};

use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use tauri::ipc::Channel;
use tauri::State;
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::process::{Child, ChildStdin};
use tokio::sync::Mutex;

use crate::sidecar;

#[derive(Default)]
pub struct Mcp(Mutex<Option<Server>>);

pub struct Server {
    transport: Transport,
    _child: Child, // kill_on_drop: replacing the Server stops the old process
}

enum Transport {
    Stdio(ChildStdin),
    Http { port: u16, token: String },
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct McpStartRequest {
    /// The workspace root: the directory served, and the server's cwd.
    pub cwd: String,
    /// "stdio" or "http".
    pub transport: String,
    /// HTTP only: the bearer token the server requires.
    #[serde(default)]
    pub token: Option<String>,
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct McpStarted {
    /// HTTP only: the endpoint, for pointing another client at it.
    pub url: Option<String>,
}

/// One HTTP reply. Header names are lowercased.
#[derive(Serialize, Debug, PartialEq)]
pub struct HttpReply {
    pub status: u16,
    pub headers: Vec<(String, String)>,
    pub body: String,
}

/// The `mhl` argument list for a transport.
pub fn serve_args(transport: &str, port: u16) -> Result<Vec<String>, String> {
    let mut args: Vec<String> = vec!["serve".into(), "mcp".into()];
    match transport {
        "stdio" => {}
        "http" => {
            args.push("--http".into());
            args.push("--addr".into());
            args.push(format!("127.0.0.1:{port}"));
        }
        other => {
            return Err(format!(
                "unknown MCP transport {other:?} (want stdio or http)"
            ))
        }
    }
    args.push(".".into());
    Ok(args)
}

/// A loopback port nothing listens on right now.
fn free_port() -> Result<u16, String> {
    TcpListener::bind((Ipv4Addr::LOCALHOST, 0))
        .and_then(|l| l.local_addr())
        .map(|a| a.port())
        .map_err(|e| format!("finding a free port: {e}"))
}

/// (Re)starts the server for `request.cwd`. The previous one, if any, is
/// stopped first. For HTTP, returns once the server answers `/readyz`.
#[tauri::command]
pub async fn mcp_start(
    mcp: State<'_, Mcp>,
    request: McpStartRequest,
    on_event: Channel<Value>,
) -> Result<McpStarted, String> {
    mcp.0.lock().await.take();

    let http = request.transport == "http";
    let port = if http { free_port()? } else { 0 };
    let token = request.token.clone().filter(|t| !t.is_empty());
    if http && token.is_none() {
        return Err("an HTTP server needs a token".into());
    }
    let mut cmd = sidecar::command(serve_args(&request.transport, port)?)?;
    cmd.current_dir(&request.cwd);
    if let Some(t) = &token {
        cmd.env("MHL_SERVE_TOKEN", t);
    }
    let mut child = cmd
        .spawn()
        .map_err(|e| format!("starting mhl serve mcp: {e}"))?;
    let stdin = child.stdin.take().ok_or("mhl serve mcp: no stdin")?;
    let stdout = child.stdout.take().ok_or("mhl serve mcp: no stdout")?;
    let stderr = child.stderr.take().ok_or("mhl serve mcp: no stderr")?;

    let out_channel = on_event.clone();
    let out_task = tauri::async_runtime::spawn(async move {
        let mut lines = BufReader::new(stdout).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            // stdout is the protocol stream over stdio; in HTTP mode it is
            // only ever diagnostics.
            let kind = if http { "log" } else { "message" };
            let _ = out_channel.send(json!({ "type": kind, "text": line }));
        }
    });
    let err_channel = on_event.clone();
    tauri::async_runtime::spawn(async move {
        let mut lines = BufReader::new(stderr).lines();
        while let Ok(Some(line)) = lines.next_line().await {
            let _ = err_channel.send(json!({ "type": "log", "text": line }));
        }
        let _ = out_task.await;
        let _ = err_channel.send(json!({ "type": "exit" }));
    });

    let transport = if http {
        wait_ready(&mut child, port).await?;
        Transport::Http {
            port,
            token: token.unwrap_or_default(),
        }
    } else {
        Transport::Stdio(stdin)
    };
    *mcp.0.lock().await = Some(Server {
        transport,
        _child: child,
    });
    Ok(McpStarted {
        url: http.then(|| format!("http://127.0.0.1:{port}/mcp")),
    })
}

/// Polls `/readyz` until the server answers 200, it exits, or 20s pass.
async fn wait_ready(child: &mut Child, port: u16) -> Result<(), String> {
    let deadline = Instant::now() + Duration::from_secs(20);
    loop {
        if let Ok(Some(status)) = child.try_wait() {
            return Err(format!(
                "mhl serve mcp exited before it was ready ({status}) — see the server log"
            ));
        }
        let probe = tauri::async_runtime::spawn_blocking(move || {
            http_request(port, "GET", "/readyz", &[], "")
        })
        .await
        .map_err(|e| e.to_string())?;
        if matches!(probe, Ok(HttpReply { status: 200, .. })) {
            return Ok(());
        }
        if Instant::now() > deadline {
            return Err("mhl serve mcp did not become ready within 20s".into());
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }
}

/// Writes one JSON-RPC message to a stdio server.
#[tauri::command]
pub async fn mcp_send(mcp: State<'_, Mcp>, message: String) -> Result<(), String> {
    if message.contains('\n') {
        return Err("a stdio MCP message must be a single line".into());
    }
    let mut guard = mcp.0.lock().await;
    let server = guard.as_mut().ok_or("the MCP server is not running")?;
    let Transport::Stdio(stdin) = &mut server.transport else {
        return Err("the MCP server is not on stdio".into());
    };
    let line = format!("{message}\n");
    stdin
        .write_all(line.as_bytes())
        .await
        .map_err(|e| format!("writing to mhl serve mcp: {e}"))?;
    stdin
        .flush()
        .await
        .map_err(|e| format!("writing to mhl serve mcp: {e}"))
}

/// Sends one request to the HTTP server; the bearer token is added here.
/// `path` is relative to the server (`/mcp`, `/mcp/tools/call`, `/healthz`).
#[tauri::command]
pub async fn mcp_http(
    mcp: State<'_, Mcp>,
    method: String,
    path: String,
    headers: Vec<(String, String)>,
    body: String,
) -> Result<HttpReply, String> {
    let (port, token) = {
        let guard = mcp.0.lock().await;
        match guard.as_ref().map(|s| &s.transport) {
            Some(Transport::Http { port, token }) => (*port, token.clone()),
            Some(Transport::Stdio(_)) => return Err("the MCP server is not on HTTP".into()),
            None => return Err("the MCP server is not running".into()),
        }
    };
    if !matches!(method.as_str(), "GET" | "POST" | "DELETE") {
        return Err(format!("unsupported HTTP method {method:?}"));
    }
    if !path.starts_with('/') {
        return Err(format!("the request path must start with '/': {path:?}"));
    }
    let mut all = headers;
    all.push(("Authorization".into(), format!("Bearer {token}")));
    // Blocking I/O off the async runtime; the lock is not held, so a long
    // tools/call doesn't stall status polls, and stopping the server (which
    // kills it) ends the request.
    tauri::async_runtime::spawn_blocking(move || http_request(port, &method, &path, &all, &body))
        .await
        .map_err(|e| e.to_string())?
}

/// Stops the server, if one is running.
#[tauri::command]
pub async fn mcp_stop(mcp: State<'_, Mcp>) -> Result<(), String> {
    mcp.0.lock().await.take();
    Ok(())
}

/// A minimal HTTP/1.1 client for the loopback server: one request per
/// connection (`Connection: close`). Enough for `mhl serve mcp --http`, which
/// only ever answers with a JSON body or none.
pub fn http_request(
    port: u16,
    method: &str,
    path: &str,
    headers: &[(String, String)],
    body: &str,
) -> Result<HttpReply, String> {
    let mut req = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1:{port}\r\nConnection: close\r\nContent-Length: {}\r\n",
        body.len()
    );
    for (k, v) in headers {
        if [k, v].iter().any(|s| s.contains(['\r', '\n'])) || k.contains(':') {
            return Err(format!("invalid header {k:?}"));
        }
        req.push_str(&format!("{k}: {v}\r\n"));
    }
    req.push_str("\r\n");
    req.push_str(body);

    let mut stream = TcpStream::connect((Ipv4Addr::LOCALHOST, port))
        .map_err(|e| format!("connecting to 127.0.0.1:{port}: {e}"))?;
    stream
        .write_all(req.as_bytes())
        .map_err(|e| format!("sending the request: {e}"))?;
    let mut raw = Vec::new();
    stream
        .read_to_end(&mut raw)
        .map_err(|e| format!("reading the reply: {e}"))?;
    parse_response(&raw)
}

/// Parses a complete HTTP/1.1 response: a Content-Length, chunked, or
/// read-to-close body.
pub fn parse_response(raw: &[u8]) -> Result<HttpReply, String> {
    let split = raw
        .windows(4)
        .position(|w| w == b"\r\n\r\n")
        .ok_or("a truncated HTTP reply (no end of headers)")?;
    let head = String::from_utf8_lossy(&raw[..split]);
    let rest = &raw[split + 4..];
    let mut lines = head.split("\r\n");
    let status_line = lines.next().unwrap_or_default();
    let status: u16 = status_line
        .split_whitespace()
        .nth(1)
        .and_then(|s| s.parse().ok())
        .ok_or_else(|| format!("a malformed HTTP status line: {status_line:?}"))?;
    let headers: Vec<(String, String)> = lines
        .filter_map(|l| l.split_once(':'))
        .map(|(k, v)| (k.trim().to_ascii_lowercase(), v.trim().to_string()))
        .collect();
    let header = |name: &str| {
        headers
            .iter()
            .find(|(k, _)| k == name)
            .map(|(_, v)| v.as_str())
    };

    let body = if header("transfer-encoding").is_some_and(|v| v.eq_ignore_ascii_case("chunked")) {
        dechunk(rest)?
    } else if let Some(len) = header("content-length").and_then(|v| v.parse::<usize>().ok()) {
        rest.get(..len)
            .ok_or("a truncated HTTP reply (body shorter than Content-Length)")?
            .to_vec()
    } else {
        rest.to_vec()
    };
    Ok(HttpReply {
        status,
        headers,
        body: String::from_utf8_lossy(&body).into_owned(),
    })
}

fn dechunk(mut rest: &[u8]) -> Result<Vec<u8>, String> {
    let mut out = Vec::new();
    loop {
        let eol = rest
            .windows(2)
            .position(|w| w == b"\r\n")
            .ok_or("a truncated chunked body")?;
        let size_line = String::from_utf8_lossy(&rest[..eol]);
        let size_hex = size_line.split(';').next().unwrap_or_default().trim();
        let size = usize::from_str_radix(size_hex, 16)
            .map_err(|_| format!("a malformed chunk size: {size_hex:?}"))?;
        rest = &rest[eol + 2..];
        if size == 0 {
            return Ok(out);
        }
        let chunk = rest.get(..size).ok_or("a truncated chunk")?;
        out.extend_from_slice(chunk);
        rest = rest.get(size + 2..).ok_or("a truncated chunk")?;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn builds_serve_args() {
        assert_eq!(serve_args("stdio", 0).unwrap(), ["serve", "mcp", "."]);
        assert_eq!(
            serve_args("http", 4100).unwrap(),
            ["serve", "mcp", "--http", "--addr", "127.0.0.1:4100", "."]
        );
        assert!(serve_args("ws", 0).is_err());
    }

    #[test]
    fn parses_a_content_length_reply() {
        let raw = b"HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nMcp-Session-Id: abc\r\nContent-Length: 2\r\n\r\n{}trailing";
        let r = parse_response(raw).unwrap();
        assert_eq!(r.status, 200);
        assert_eq!(r.body, "{}");
        assert!(r.headers.contains(&("mcp-session-id".into(), "abc".into())));
    }

    #[test]
    fn parses_a_chunked_reply() {
        let raw = b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n4\r\n{\"a\"\r\n3;x=y\r\n:1}\r\n0\r\n\r\n";
        assert_eq!(parse_response(raw).unwrap().body, r#"{"a":1}"#);
    }

    #[test]
    fn parses_an_empty_202() {
        let r = parse_response(b"HTTP/1.1 202 Accepted\r\nContent-Length: 0\r\n\r\n").unwrap();
        assert_eq!((r.status, r.body.as_str()), (202, ""));
    }

    #[test]
    fn parses_a_read_to_close_body() {
        let r = parse_response(b"HTTP/1.0 404 Not Found\r\n\r\nnope").unwrap();
        assert_eq!((r.status, r.body.as_str()), (404, "nope"));
    }

    #[test]
    fn rejects_truncated_replies() {
        assert!(parse_response(b"HTTP/1.1 200 OK\r\nContent-Length: 9").is_err());
        assert!(parse_response(b"HTTP/1.1 200 OK\r\nContent-Length: 9\r\n\r\n{}").is_err());
        assert!(parse_response(b"garbage\r\n\r\n").is_err());
    }

    #[test]
    fn refuses_header_injection() {
        let bad = [("X".to_string(), "a\r\nB: c".to_string())];
        assert!(http_request(1, "GET", "/", &bad, "").is_err());
    }

    fn sidecar() -> Option<std::path::PathBuf> {
        let bin = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("binaries");
        let found = std::fs::read_dir(&bin)
            .ok()
            .and_then(|mut d| d.find_map(|e| e.ok().map(|e| e.path())));
        if found.is_none() {
            eprintln!(
                "skipping: no sidecar in {} (run `npm run sidecar`)",
                bin.display()
            );
        }
        found
    }

    /// A fresh workspace per test (`name` tells them apart): the tests run
    /// in parallel and each removes its own when done, so a shared one
    /// could vanish under a server still starting.
    fn workspace(name: &str) -> std::path::PathBuf {
        let dir = std::env::temp_dir().join(format!("mhl-studio-mcp-{}-{name}", std::process::id()));
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(
            dir.join("main.mh"),
            "workflow Greet {\n  input name: string = \"world\"\n  var greeting = \"\"\n  step Hello {\n    greeting = \"hello ${name}\"\n  }\n}\n",
        )
        .unwrap();
        dir
    }

    /// The real vendored server over stdio: initialize, then a tool call.
    #[tokio::test]
    async fn talks_to_the_vendored_server_over_stdio() {
        let Some(mhl) = sidecar() else { return };
        let dir = workspace("stdio");
        let mut child = tokio::process::Command::new(mhl)
            .args(serve_args("stdio", 0).unwrap())
            .current_dir(&dir)
            .stdin(std::process::Stdio::piped())
            .stdout(std::process::Stdio::piped())
            .stderr(std::process::Stdio::null())
            .kill_on_drop(true)
            .spawn()
            .unwrap();
        let mut stdin = child.stdin.take().unwrap();
        let mut out = BufReader::new(child.stdout.take().unwrap()).lines();
        for msg in [
            r#"{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}"#,
            r#"{"jsonrpc":"2.0","method":"notifications/initialized"}"#,
            r#"{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Greet","arguments":{"name":"mcp"}}}"#,
        ] {
            stdin
                .write_all(format!("{msg}\n").as_bytes())
                .await
                .unwrap();
        }
        let init: Value = serde_json::from_str(&out.next_line().await.unwrap().unwrap()).unwrap();
        assert_eq!(init["result"]["serverInfo"]["name"], "mhl");
        let call: Value = serde_json::from_str(&out.next_line().await.unwrap().unwrap()).unwrap();
        assert_eq!(call["id"], 2);
        assert_eq!(call["result"]["structuredContent"]["greeting"], "hello mcp");
        let _ = std::fs::remove_dir_all(dir);
    }

    /// The real vendored server over HTTP, through this module's client:
    /// the token is enforced and initialize mints a session.
    #[tokio::test]
    async fn talks_to_the_vendored_server_over_http() {
        let Some(mhl) = sidecar() else { return };
        let dir = workspace("http");
        let port = free_port().unwrap();
        let mut child = tokio::process::Command::new(mhl)
            .args(serve_args("http", port).unwrap())
            .current_dir(&dir)
            .env("MHL_SERVE_TOKEN", "t0ken")
            .stdout(std::process::Stdio::null())
            .stderr(std::process::Stdio::null())
            .kill_on_drop(true)
            .spawn()
            .unwrap();
        wait_ready(&mut child, port).await.unwrap();
        let init = r#"{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{}}}"#;
        let json = [("Content-Type".to_string(), "application/json".to_string())];
        let denied = http_request(port, "POST", "/mcp", &json, init).unwrap();
        assert_eq!(denied.status, 401);
        let mut auth = json.to_vec();
        auth.push(("Authorization".into(), "Bearer t0ken".into()));
        let r = http_request(port, "POST", "/mcp", &auth, init).unwrap();
        assert_eq!(r.status, 200);
        assert!(r
            .headers
            .iter()
            .any(|(k, v)| k == "mcp-session-id" && !v.is_empty()));
        let v: Value = serde_json::from_str(&r.body).unwrap();
        assert!(v["result"]["capabilities"]["experimental"]["mhl.run"].is_object());
        let _ = std::fs::remove_dir_all(dir);
    }
}
