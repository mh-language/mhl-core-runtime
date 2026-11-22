//! The open folder: listing its files and folders as they are on disk (the
//! side bar mirrors the workspace), and reading/writing them.

use std::fs;
use std::path::{Path, PathBuf};

/// Never listed: version-control internals and OS litter.
const SKIP: &[&str] = &[".git", ".hg", ".svn", ".DS_Store", "Thumbs.db"];

/// Folders listed but not walked into: dependencies and build output can hold
/// tens of thousands of files. A Python virtualenv (a folder holding
/// `pyvenv.cfg`, whatever its name) is one too.
const HEAVY_DIRS: &[&str] = &["node_modules", "target", "dist", "vendor", ".venv", "venv"];

/// At most this many files and folders are listed, so a huge folder opened by
/// mistake can't freeze the side bar; `Listing::truncated` then says so.
const MAX_ENTRIES: usize = 20_000;

/// Files larger than this are not opened in the editor.
const MAX_OPEN_BYTES: u64 = 5 * 1024 * 1024;

/// What `list_files` returns: relative ("/"-separated), sorted paths.
#[derive(serde::Serialize, Debug, PartialEq)]
pub struct Listing {
    /// Every file.
    pub files: Vec<String>,
    /// Every folder, empty ones included (so a folder just created shows).
    pub dirs: Vec<String>,
    /// Folders listed without their contents (dependencies, build output).
    pub unlisted: Vec<String>,
    /// The workspace has more than `MAX_ENTRIES` entries: the rest are not listed.
    pub truncated: bool,
}

/// Every file and folder under `root`, as on disk — hidden ones included —
/// but version-control internals and OS litter (`SKIP`). Dependency and build
/// folders are listed without their contents (`Listing::unlisted`).
#[tauri::command]
pub fn list_files(root: String) -> Result<Listing, String> {
    let root = PathBuf::from(root);
    let mut out = Listing {
        files: Vec::new(),
        dirs: Vec::new(),
        unlisted: Vec::new(),
        truncated: false,
    };
    walk(&root, &root, &mut out).map_err(|e| format!("listing {}: {e}", root.display()))?;
    out.files.sort();
    out.dirs.sort();
    out.unlisted.sort();
    Ok(out)
}

fn rel(root: &Path, path: &Path) -> Option<String> {
    let rel = path.strip_prefix(root).ok()?;
    let parts: Vec<_> = rel
        .iter()
        .map(|p| p.to_string_lossy().into_owned())
        .collect();
    Some(parts.join("/"))
}

fn heavy(name: &str, path: &Path) -> bool {
    HEAVY_DIRS.contains(&name) || path.join("pyvenv.cfg").is_file()
}

fn walk(root: &Path, dir: &Path, out: &mut Listing) -> std::io::Result<()> {
    let mut entries: Vec<_> = fs::read_dir(dir)?.collect::<Result<_, _>>()?;
    entries.sort_by_key(|e| e.file_name());
    for entry in entries {
        if out.files.len() + out.dirs.len() >= MAX_ENTRIES {
            out.truncated = true;
            return Ok(());
        }
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if SKIP.contains(&name.as_ref()) {
            continue;
        }
        let path = entry.path();
        // A symlink is listed as what it points to; a broken one as a file.
        let is_dir = path.is_dir();
        let Some(r) = rel(root, &path) else { continue };
        if is_dir {
            // A symlinked folder is listed, not walked: it may point back up.
            if heavy(&name, &path) || entry.file_type()?.is_symlink() {
                out.unlisted.push(r.clone());
                out.dirs.push(r);
            } else {
                out.dirs.push(r);
                walk(root, &path, out)?;
            }
        } else {
            out.files.push(r);
        }
    }
    Ok(())
}

/// The text of `path`, for the editor. A binary file (one with a NUL byte or
/// that is not UTF-8) or one over `MAX_OPEN_BYTES` is refused with a message
/// saying so, starting with "not a text file" / "too large".
#[tauri::command]
pub fn read_file(path: String) -> Result<String, String> {
    let meta = fs::metadata(&path).map_err(|e| format!("reading {path}: {e}"))?;
    if meta.len() > MAX_OPEN_BYTES {
        return Err(format!(
            "too large to open here: {path} is {} MB",
            meta.len() / (1024 * 1024)
        ));
    }
    let bytes = fs::read(&path).map_err(|e| format!("reading {path}: {e}"))?;
    if bytes.iter().take(8192).any(|&b| b == 0) {
        return Err(format!("not a text file: {path}"));
    }
    String::from_utf8(bytes).map_err(|_| format!("not a text file: {path} is not UTF-8"))
}

#[tauri::command]
pub fn write_file(path: String, contents: String) -> Result<(), String> {
    fs::write(&path, contents).map_err(|e| format!("writing {path}: {e}"))
}

/// Creates `path` with `contents`, refusing to overwrite an existing file.
#[tauri::command]
pub fn create_file(path: String, contents: String) -> Result<(), String> {
    let p = Path::new(&path);
    if p.exists() {
        return Err(format!("{path} already exists"));
    }
    if let Some(parent) = p.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("creating {}: {e}", parent.display()))?;
    }
    fs::write(p, contents).map_err(|e| format!("writing {path}: {e}"))
}

/// Creates the folder `path` (and its parents); an existing one is an error.
#[tauri::command]
pub fn create_dir(path: String) -> Result<(), String> {
    let p = Path::new(&path);
    if p.exists() {
        return Err(format!("{path} already exists"));
    }
    fs::create_dir_all(p).map_err(|e| format!("creating {path}: {e}"))
}

/// Moves or renames a file or folder. Refuses to overwrite: the target must
/// not exist. Missing parent folders of the target are created.
#[tauri::command]
pub fn rename_path(from: String, to: String) -> Result<(), String> {
    let (src, dst) = (Path::new(&from), Path::new(&to));
    if !src.exists() {
        return Err(format!("{from} does not exist"));
    }
    if dst.exists() {
        return Err(format!("{to} already exists"));
    }
    if dst.starts_with(src) {
        return Err(format!("cannot move {from} into itself"));
    }
    if let Some(parent) = dst.parent() {
        fs::create_dir_all(parent).map_err(|e| format!("creating {}: {e}", parent.display()))?;
    }
    fs::rename(src, dst).map_err(|e| format!("moving {from} to {to}: {e}"))
}

/// Deletes a file or folder by moving it to the system Trash (Recycle Bin),
/// so a mistake can be undone from there.
#[tauri::command]
pub fn delete_path(path: String) -> Result<(), String> {
    if !Path::new(&path).exists() {
        return Err(format!("{path} does not exist"));
    }
    trash::delete(&path).map_err(|e| format!("moving {path} to the Trash: {e}"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn lists_every_file_and_folder_as_on_disk() {
        let dir = std::env::temp_dir().join(format!("mhl-studio-ws-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        for f in [
            "a.mh",
            "agilespec",
            "check.sh",
            ".gitignore",
            "sub/notes.md",
            "sub/image.png",
            ".mhl/state/x.json",
            ".git/HEAD",
            ".DS_Store",
            "node_modules/y/index.js",
            "py/.env/pyvenv.cfg",
            "py/.env/lib/z.py",
            ".env",
        ] {
            let p = dir.join(f);
            fs::create_dir_all(p.parent().unwrap()).unwrap();
            fs::write(p, "").unwrap();
        }
        assert!(create_dir(dir.join("prompts/empty").to_string_lossy().into_owned()).is_ok());
        assert!(create_dir(dir.join("sub").to_string_lossy().into_owned()).is_err());
        let l = list_files(dir.to_string_lossy().into_owned()).unwrap();
        // Hidden files show; .git and .DS_Store never do.
        assert_eq!(
            l.files,
            vec![
                ".env",
                ".gitignore",
                ".mhl/state/x.json",
                "a.mh",
                "agilespec",
                "check.sh",
                "sub/image.png",
                "sub/notes.md"
            ]
        );
        // Dependencies and a virtualenv (whatever its name) are listed, not walked.
        assert_eq!(
            l.dirs,
            vec![
                ".mhl",
                ".mhl/state",
                "node_modules",
                "prompts",
                "prompts/empty",
                "py",
                "py/.env",
                "sub"
            ]
        );
        assert_eq!(l.unlisted, vec!["node_modules", "py/.env"]);
        assert!(!l.truncated);
        assert!(create_file(
            dir.join("a.mh").to_string_lossy().into_owned(),
            String::new()
        )
        .is_err());
        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn reads_text_and_refuses_binary_files() {
        let dir = std::env::temp_dir().join(format!("mhl-studio-rd-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(&dir).unwrap();
        let p = |s: &str| dir.join(s).to_string_lossy().into_owned();
        fs::write(dir.join("t.py"), "print('olá')\n").unwrap();
        fs::write(dir.join("i.png"), [0x89, b'P', b'N', b'G', 0, 1, 2]).unwrap();
        fs::write(dir.join("l1.txt"), [0xe9, b'a']).unwrap();
        assert_eq!(read_file(p("t.py")).unwrap(), "print('olá')\n");
        assert!(read_file(p("i.png"))
            .unwrap_err()
            .starts_with("not a text file"));
        assert!(read_file(p("l1.txt"))
            .unwrap_err()
            .starts_with("not a text file"));
        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn renames_and_moves_without_overwriting() {
        let dir = std::env::temp_dir().join(format!("mhl-studio-mv-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        fs::create_dir_all(dir.join("lib")).unwrap();
        fs::write(dir.join("a.mh"), "a").unwrap();
        fs::write(dir.join("b.mh"), "b").unwrap();
        let p = |s: &str| dir.join(s).to_string_lossy().into_owned();
        assert!(
            rename_path(p("a.mh"), p("b.mh")).is_err(),
            "must not overwrite"
        );
        rename_path(p("a.mh"), p("lib/deep/a.mh")).unwrap();
        assert_eq!(fs::read_to_string(dir.join("lib/deep/a.mh")).unwrap(), "a");
        assert!(
            rename_path(p("lib"), p("lib/inner")).is_err(),
            "must not move into itself"
        );
        rename_path(p("lib"), p("src")).unwrap();
        assert!(dir.join("src/deep/a.mh").exists());
        assert!(rename_path(p("gone.mh"), p("x.mh")).is_err());
        fs::remove_dir_all(&dir).unwrap();
    }
}
