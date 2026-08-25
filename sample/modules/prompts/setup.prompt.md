---
name: setup-prompt
description: "inspect a Specification handoff, prepare its concrete development environment, and preserve the approved feature plan"
---

The Specification handoff is authoritative for scope, feature boundaries, priorities,dependencies, references, requirements, constraints, and acceptance criteria. This skill performs the same target classification, Git preparation, and deterministic setup as dev-initializer, but it does not create or modify the feature plan.

## 1. Classify the target

Determine whether the target is:

  - **Greenfield:** no existing application. Use app/<descriptive-name> as the target and create the minimal project structure required by the approved handoff.
  
  - **Brownfield:** an application already exists. Inspect only the metadata needed to prepare the approved work: the top-level layout, README, existing docs/ADRs, build manifests, bootstrap or verification scripts, and progress.txt. Reuse the established target and verification command when they exist. Do not inventory the whole source tree.

  The handoff's features are already sliced and ordered. Do not re-plan the existing app, reinterpret the readiness slices, merge or split features, change priorities, change dependencies, or add unrelated scope.
  
## 2. Prepare Git

Initialize Git if necessary. Never work directly on main or master. Reuse an existing non-default branch when resuming; otherwise create `<YYYYMMDDHHMM>-<descriptive-name>`, using the current UTC timestamp. Keep the target directory and the harness repository boundary clear. The target directory reported at the end is the directory in which the harness will look for init.sh, verify-feature.sh, progress.txt, and the project's verification command.
  
## 3. Scaffold deterministic setup

  - init.sh and verify-feature.sh MUST live directly inside the exact directory reported as `TARGET_DIR` — never at the repo root, a parent directory, or another nesting level. The harness locates them by joining the reported `TARGET_DIR` with these exact filenames; a script placed anywhere else is invisible to it and silently skipped in favor of a weaker fallback. If setup that lives outside `TARGET_DIR` is required (for example, docker-compose.yml or infrastructure shared with siblings), reference it from inside these scripts via a relative path 
  - the scripts themselves still belong in `TARGET_DIR`.
  - Ensure an idempotent `init.sh` prepares, builds, and when applicable starts the project.
  - Ensure `init.sh` is safe to run repeatedly and returns non-zero when setup, build, or readiness fails.
  - Ensure an idempotent `verify-feature.sh <feature-id>` runs the established verification path. It may run the full suite when there is no per-feature convention.
  - Ensure `verify-feature.sh <feature-id>` uses the feature id to select a narrower check when the project supports feature-level verification, but never reports success without running a real check.
  - Make the verifier print a concise `PASS: ...` or `FAIL: ...` verdict and use its process exit code as evidence. The harness captures detailed output separately.
  - `TARGET_DIR`'s verification command and verify-feature.sh must actually exercise the code using the real build/test/lint pipeline — for example dotnet test, npm test, or pytest.
  - A placeholder that always succeeds regardless of what was implemented (echo, true, exit 0, or an empty script) is never acceptable, even for the first feature of a greenfield target. If no test suite exists yet, creating one that actually runs is part of the first approved feature's implementation, not something to defer by faking a pass.
  - In brownfield targets, reuse equivalent scripts or pipelines and make only the minimum adaptation needed. Do not overwrite working project setup.
  - Verify that the reported target directory exists, that both scripts are executable, and that the command can be run from that directory.
  
## 4. Preserve the approved handoff

Do not write `*/plan.json`, do not return plan, and do not modify .`*/feature_list.json`. The harness has already imported the approved features. You may make only operational changes required to prepare the target:

  - create the application scaffold for a greenfield target;
  - create or repair setup and verification scripts;
  - configure the local environment;
  - choose the concrete target path;
  - choose the executable verification command.

Do not return semantic descriptions such as "local .NET API with Postgres" as `TARGET_DIR`.Return a real path such as app/todo-api. Return setup with exactly two arguments, in this order:
  
  1. `TARGET_DIR`: the concrete target directory, relative to the harness root when possible;
  2. `VERIFY_CMD`: the executable verification command, without prose.

