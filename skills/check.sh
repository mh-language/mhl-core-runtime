#!/bin/sh
# Keeps the skills honest: every ```mh block in a SKILL.md or reference file is
# a complete program, so each one must pass `mhl lint`, and each one declaring a
# `test` must pass `mhl test`. Every SKILL.md must also load through mhl's own
# `skill Name from "..."` declaration.
#
# Excerpts that cannot stand alone (several files, a shell session) are fenced
# as ```mh-fragment and are skipped here.
#
# Usage: skills/check.sh            uses `mhl` on PATH
#        MHL=path/to/mhl skills/check.sh
set -eu

MHL=${MHL:-mhl}
case $MHL in
*/*) MHL=$(cd "$(dirname "$MHL")" && pwd)/$(basename "$MHL") ;;
esac
root=$(cd "$(dirname "$0")" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

blocks=0
failed=0

report() { # label, captured output
	failed=$((failed + 1))
	printf 'FAIL %s\n%s\n\n' "$1" "$2"
}

for doc in "$root"/*/SKILL.md "$root"/*/references/*.md; do
	[ -f "$doc" ] || continue
	label=${doc#"$root"/}
	dir=$work/$(printf '%s' "$label" | tr '/.' '__')
	mkdir -p "$dir"
	awk -v dir="$dir" '
		/^```mh$/ { out = sprintf("%s/L%04d.mh", dir, NR); next }
		/^```$/   { if (out) { close(out); out = "" } next }
		out       { print > out }
	' "$doc"
	for block in "$dir"/L*.mh; do
		[ -f "$block" ] || continue
		blocks=$((blocks + 1))
		name=$(basename "$block" .mh)
		line=$(printf '%s' "${name#L}" | sed 's/^0*//')
		case_dir=$dir/$name
		mkdir "$case_dir"
		mv "$block" "$case_dir/main.mh"
		if ! output=$(cd "$case_dir" && "$MHL" lint main.mh 2>&1); then
			report "$label:$line (lint)" "$output"
			continue
		fi
		if grep -q '^test ' "$case_dir/main.mh"; then
			if ! output=$(cd "$case_dir" && "$MHL" test main.mh 2>&1); then
				report "$label:$line (test)" "$output"
			fi
		fi
	done
done

loader=$work/load.mh
n=0
for skill in "$root"/*/SKILL.md; do
	n=$((n + 1))
	cp "$skill" "$work/skill$n.md"
	printf 'skill Skill%d from "skill%d.md"\n' "$n" "$n" >>"$loader"
done
if ! output=$("$MHL" lint "$loader" 2>&1); then
	report "skill declarations" "$output"
fi

printf '%d block(s) checked across %d skill(s), %d failure(s)\n' "$blocks" "$n" "$failed"
[ "$failed" -eq 0 ]
