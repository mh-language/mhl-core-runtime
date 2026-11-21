// mhl source file extensions, mirroring the runtime's internal/lang/mhfile:
// ".mh" is canonical (what the studio writes for a new file), ".mhl" an alias.

/** Every accepted mhl source extension, canonical first. */
export const MH_EXTENSIONS = [".mh", ".mhl"];

/** The extension the studio gives a new mhl file. */
export const MH_EXT = MH_EXTENSIONS[0];

const MH_SUFFIX = /\.mhl?$/;

/** Whether `path` (a name or a path) ends in an mhl source extension. */
export function isMhPath(path: string): boolean {
  return MH_SUFFIX.test(path);
}

/** `path` without its mhl source extension: `flows/a.mhl` → `flows/a`. */
export function stripMhExt(path: string): string {
  return path.replace(MH_SUFFIX, "");
}
