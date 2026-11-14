//! mhl studio backend: a thin layer between the webview and the vendored `mhl`
//! binary. Every language decision — parsing, linting, the workflow graph,
//! execution — is made by `mhl` itself (`graph`, `run --format jsonl`,
//! `run --dry-run`, `lsp`); this crate only spawns it, pipes data and
//! streams events back to the UI.

mod lsp;
mod mhl;
mod run;
mod shellenv;
mod sidecar;
mod workspace;

pub fn run() {
    // Before anything spawns: the agent CLIs live on the user's shell PATH.
    shellenv::import_login_path();
    tauri::Builder::default()
        .plugin(tauri_plugin_dialog::init())
        .manage(run::Runs::default())
        .manage(lsp::Lsp::default())
        .invoke_handler(tauri::generate_handler![
            workspace::list_files,
            workspace::read_file,
            workspace::write_file,
            workspace::create_file,
            workspace::create_dir,
            workspace::rename_path,
            workspace::delete_path,
            mhl::mhl_version,
            mhl::graph,
            mhl::inspect,
            mhl::evaluate,
            run::run_start,
            run::run_cancel,
            lsp::lsp_start,
            lsp::lsp_send,
            lsp::lsp_stop,
            shellenv::find_clis,
        ])
        .run(tauri::generate_context!())
        .expect("error while running mhl studio");
}
