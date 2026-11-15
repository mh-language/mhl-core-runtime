//! The open folder: listing its `.mh` and `.md` files (and folders), and
//! reading/writing them.

use std::fs;
use std::path::{Path, PathBuf};

/// Directories never worth descending into when looking for `.mh` files.
const SKIP_DIRS: &[&str] = &["node_modules", "target", "dist", "vendor"];

/// The file kinds the studio shows: mhl source, Markdown (prompt bodies
/// loaded with `prompt X(...) from "x.md"`, skills, notes) and JSON (JSON
/// Schemas loaded with `schema X from "x.json"`).
const EXTENSIONS: &[&str] = &[".mh", ".md", ".json"];

/// What `list_files` returns: relative ("/"-separated), sorted paths.
#[derive(serde::Serialize, Debug, PartialEq)]
pub struct Listing {
    /// Every `.mh`, `.md` and `.json` file.
    pub files: Vec<String>,
    /// Every folder, empty ones included (so a folder just created shows).
    pub dirs: Vec<String>,
}

/// Every `.mh`/`.md`/`.json` file and folder under `root`. Hidden entries (`.git`,
/// `.mhl` state, ...) and vendor/build folders are skipped.
#[tauri::command]
pub fn list_files(root: String) -> Result<Listing, String> {
    let root = PathBuf::from(root);
    let mut out = Listing { files: Vec::new(), dirs: Vec::new() };
    walk(&root, &root, &mut out).map_err(|e| format!("listing {}: {e}", root.display()))?;
    out.files.sort();
    out.dirs.sort();
    Ok(out)
}

fn rel(root: &Path, path: &Path) -> Option<String> {
    let rel = path.strip_prefix(root).ok()?;
    let parts: Vec<_> = rel.iter().map(|p| p.to_string_lossy().into_owned()).collect();
    Some(parts.join("/"))
}

fn walk(root: &Path, dir: &Path, out: &mut Listing) -> std::io::Result<()> {
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let name = entry.file_name();
        let name = name.to_string_lossy();
        if name.starts_with('.') {
            continue;
        }
        let ty = entry.file_type()?;
        let path = entry.path();
        if ty.is_dir() {
            if !SKIP_DIRS.contains(&name.as_ref()) {
                if let Some(r) = rel(root, &path) {
                    out.dirs.push(r);
                }
                walk(root, &path, out)?;
            }
        } else if ty.is_file() && EXTENSIONS.iter().any(|ext| name.ends_with(ext)) {
            if let Some(r) = rel(root, &path) {
                out.files.push(r);
            }
        }
    }
    Ok(())
}

#[tauri::command]
pub fn read_file(path: String) -> Result<String, String> {
    fs::read_to_string(&path).map_err(|e| format!("reading {path}: {e}"))
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
    fn lists_mh_md_and_json_files_and_dirs_skipping_hidden_and_vendor_dirs() {
        let dir = std::env::temp_dir().join(format!("mhl-studio-ws-{}", std::process::id()));
        let _ = fs::remove_dir_all(&dir);
        for f in [
            "a.mh",
            "sub/b.mh",
            "sub/notes.md",
            "sub/data.json",
            "sub/image.png",
            ".mhl/state/x.mh",
            "node_modules/y.mh",
        ] {
            let p = dir.join(f);
            fs::create_dir_all(p.parent().unwrap()).unwrap();
            fs::write(p, "").unwrap();
        }
        assert!(create_dir(dir.join("prompts/empty").to_string_lossy().into_owned()).is_ok());
        assert!(create_dir(dir.join("sub").to_string_lossy().into_owned()).is_err());
        let l = list_files(dir.to_string_lossy().into_owned()).unwrap();
        assert_eq!(l.files, vec!["a.mh", "sub/b.mh", "sub/data.json", "sub/notes.md"]);
        assert_eq!(l.dirs, vec!["prompts", "prompts/empty", "sub"]);
        assert!(create_file(
            dir.join("a.mh").to_string_lossy().into_owned(),
            String::new()
        )
        .is_err());
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
        assert!(rename_path(p("a.mh"), p("b.mh")).is_err(), "must not overwrite");
        rename_path(p("a.mh"), p("lib/deep/a.mh")).unwrap();
        assert_eq!(fs::read_to_string(dir.join("lib/deep/a.mh")).unwrap(), "a");
        assert!(rename_path(p("lib"), p("lib/inner")).is_err(), "must not move into itself");
        rename_path(p("lib"), p("src")).unwrap();
        assert!(dir.join("src/deep/a.mh").exists());
        assert!(rename_path(p("gone.mh"), p("x.mh")).is_err());
        fs::remove_dir_all(&dir).unwrap();
    }
}
