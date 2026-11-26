#!/usr/bin/env python3
"""Watch this repo's GitHub Actions runs and releases without the gh CLI.

Talks to the public GitHub REST API with the standard library only. The repo
is public, so no token is needed; GITHUB_TOKEN, when set, is sent to raise
the rate limit (60 requests/hour without it) and is never printed. Polling
uses conditional requests (ETag): a 304 does not count against the limit.

    actions.py run <workflow.yml> <ref> [--max-wait SECONDS] [--interval SECONDS]
        Wait for the push run of <workflow.yml> for <ref>, a tag (v1.16.0) or
        a branch (main) whose current commit is the one to watch. It finds the
        run for that commit (retrying while it has not appeared yet), then
        polls until it completes or --max-wait elapses (default 540s, under a
        10-minute tool timeout).

    actions.py release <tag>
        Show a published release: draft/prerelease flags and asset names.

Exit codes: 0 success (run succeeded / release found), 1 the run did not
succeed, 2 no run or release found, 3 still running when --max-wait elapsed
(call again), 4 API error (rate limit, network).
"""

import argparse
import json
import os
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request

API = "https://api.github.com"


def slug():
    url = subprocess.run(["git", "remote", "get-url", "origin"], capture_output=True, text=True, check=True).stdout.strip()
    m = re.search(r"github\.com[:/]([^/]+/[^/]+?)(?:\.git)?$", url)
    if not m:
        sys.exit(f"actions: origin is not a GitHub remote: {url}")
    return m.group(1)


def commit_of(ref):
    # A tag pushed by a release script exists locally too; fall back to the remote.
    out = subprocess.run(["git", "rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}"], capture_output=True, text=True)
    if out.returncode == 0:
        return out.stdout.strip()
    out = subprocess.run(["git", "ls-remote", "origin", f"refs/tags/{ref}^{{}}", f"refs/tags/{ref}", f"refs/heads/{ref}"],
                         capture_output=True, text=True, check=True).stdout.split()
    if not out:
        sys.exit(f"actions: {ref} is neither a local ref nor on origin")
    return out[0]


_etags = {}


def get(path):
    """GET path; returns (status, json). Reuses the last body on a 304."""
    req = urllib.request.Request(API + path, headers={
        "Accept": "application/vnd.github+json",
        "X-GitHub-Api-Version": "2022-11-28",
        "User-Agent": "release-manager-agent",
    })
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    if path in _etags:
        req.add_header("If-None-Match", _etags[path][0])
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = json.load(resp)
            if resp.headers.get("ETag"):
                _etags[path] = (resp.headers["ETag"], body)
            return resp.status, body
    except urllib.error.HTTPError as e:
        if e.code == 304:
            return 200, _etags[path][1]
        if e.code == 404:
            return 404, None
        remaining = e.headers.get("X-RateLimit-Remaining")
        if e.code in (403, 429) and remaining == "0":
            reset = int(e.headers.get("X-RateLimit-Reset", "0"))
            print(f"rate limit exhausted; resets in {max(0, reset - int(time.time()))}s "
                  "(set GITHUB_TOKEN to raise it)", file=sys.stderr)
        else:
            print(f"API error {e.code} on {path}: {e.read()[:300]!r}", file=sys.stderr)
        sys.exit(4)
    except urllib.error.URLError as e:
        print(f"network error on {path}: {e.reason}", file=sys.stderr)
        sys.exit(4)


def find_run(repo, workflow, ref, sha, deadline, interval):
    path = f"/repos/{repo}/actions/workflows/{workflow}/runs?event=push&head_sha={sha}&per_page=20"
    while True:
        _, body = get(path)
        runs = [r for r in (body or {}).get("workflow_runs", []) if r.get("head_branch") == ref]
        if runs:
            return max(runs, key=lambda r: r["id"])  # newest
        if time.time() >= deadline:
            return None
        time.sleep(min(15, interval))


def report_failure(repo, run_id):
    _, body = get(f"/repos/{repo}/actions/runs/{run_id}/jobs?per_page=100")
    for job in (body or {}).get("jobs", []):
        if job.get("conclusion") not in ("success", "skipped", None):
            steps = [s["name"] for s in job.get("steps", []) if s.get("conclusion") not in ("success", "skipped", None)]
            print(f"  job {job['name']!r}: {job['conclusion']}" + (f" at step {steps[0]!r}" if steps else "") + f" — {job['html_url']}")


def cmd_run(args):
    repo = slug()
    sha = commit_of(args.ref)
    deadline = time.time() + args.max_wait
    # A just-pushed ref's run can take a while to be created.
    run = find_run(repo, args.workflow, args.ref, sha, min(deadline, time.time() + 180), args.interval)
    if run is None:
        print(f"no {args.workflow} push run for {args.ref} ({sha[:7]}) yet")
        return 2
    run_id = run["id"]
    print(f"run {run_id}: {run['html_url']}")
    last = None
    while True:
        _, run = get(f"/repos/{repo}/actions/runs/{run_id}")
        state = (run["status"], run.get("conclusion"))
        if state != last:
            print(f"  status={run['status']} conclusion={run.get('conclusion')}", flush=True)
            last = state
        if run["status"] == "completed":
            if run.get("conclusion") == "success":
                return 0
            report_failure(repo, run_id)
            return 1
        if time.time() >= deadline:
            print("still running; call again to keep waiting")
            return 3
        time.sleep(args.interval)


def cmd_release(args):
    status, rel = get(f"/repos/{slug()}/releases/tags/{args.tag}")
    if status == 404:
        # Drafts are invisible without a token: the studio's release shows up once published.
        print(f"no published release for {args.tag}")
        return 2
    print(f"{rel['html_url']}\n  draft={rel['draft']} prerelease={rel['prerelease']} assets={len(rel['assets'])}")
    for a in rel["assets"]:
        print(f"  - {a['name']} ({a['size']} bytes)")
    return 0


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    sub = p.add_subparsers(dest="cmd", required=True)
    r = sub.add_parser("run")
    r.add_argument("workflow")
    r.add_argument("ref")
    r.add_argument("--max-wait", type=int, default=540)
    r.add_argument("--interval", type=int, default=30)
    r.set_defaults(fn=cmd_run)
    rel = sub.add_parser("release")
    rel.add_argument("tag")
    rel.set_defaults(fn=cmd_release)
    args = p.parse_args()
    sys.exit(args.fn(args))


if __name__ == "__main__":
    main()
