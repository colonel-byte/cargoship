# Rendering Definitions in CI

[Generating a definition from a template](generating-definitions.md) turns a `distro.yaml` into a build output: the template and the values files are the source, and the rendered tree is produced from them. Once that is true, the render belongs to CI rather than to whoever last remembered to run it.

This page is what it takes to wire `cargoship dev render` into a GitHub Actions workflow that keeps a repository of definitions current - on a schedule, or whenever an input changes - and opens a pull request with whatever moved. It is modelled on the workflow this repository runs over its own generated examples, [`refresh-examples.yaml`](../workflows/refresh-examples.md), which has been doing exactly this weekly; the differences are called out where the operator case is simpler.

## The Shape

A definitions repository holds the inputs and the outputs side by side:

```text
.
|- templates/
|  |- distro.yaml.tmpl
|  |- values.yaml.tmpl
|- versions/
|  |- common.yaml
|  |- 1.36.4-k3s1.yaml
|  |- 1.37.1-k3s1.yaml
|- packages/                     <- rendered, never hand-edited
|  |- 1.36.4-k3s1/
|  |  |- distro.yaml
|  |  |- values.yaml
|  |- 1.37.1-k3s1/
|- .github/workflows/render.yaml
```

The workflow renders every directory under `packages/` from the matching file under `versions/`, and opens a pull request if the result differs from what is committed. Three properties are worth deciding on deliberately, because each one is a decision the pull request model makes for you:

| Property                        | Why it matters                                                                                                                                                                                                            |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| The render opens a pull request | A definition is what installs a cluster. A render that pushed to the default branch would make an upstream change into a fleet change with nobody reading the diff                                                        |
| The rendered tree is committed  | A package build, an air-gapped transfer and a `git bisect` all want the definition that was used, not the one a template would produce today. Rendering at build time instead means the artifact has no reviewable source |
| One fixed branch, force-pushed  | Successive runs collapse into a single open pull request rather than accumulating one per week. The branch is output: an edit made on it is lost on the next run, which is the intent                                     |

## Getting the Binary onto the Runner

Four ways. Reach for the first unless something below it is a better fit.

**[`colonel-byte/setup-cargoship`](https://github.com/colonel-byte/setup-cargoship), the setup action.** It downloads the release archive for the runner's OS and architecture, unpacks it, and puts `cargoship` on `PATH`:

```yaml
- name: Setup cargoship
  uses: colonel-byte/setup-cargoship@4da2adc72270551216e4c58daae04ea8e3391948 # v1.0.0
  with:
    version: ${{ vars.CARGOSHIP_VERSION }}
```

`version` is its only input and it is optional - omitted, the action resolves whatever `releases/latest` redirects to at the moment the job runs. **Pass it anyway.** A workflow whose tool version moves on its own produces a diff nobody asked for, on a day nobody expected, and the first thing anyone will want from a surprising render is to know which build produced it. A repository variable keeps that to one edit in the repository settings and prints the value in every run log.

Two things to know about it: it covers Linux and macOS on X64 and ARM64 and fails on anything else, which is every hosted runner and not a self-hosted riscv64 one; and it verifies neither the checksum nor the signature of what it downloads. Where the repository's own policy asks for that, use the next form instead.

**A pinned release archive, verified.** More lines, and the one to reach for when the download has to be checked, or when the runner's platform is outside what the action covers:

<!-- x-release-please-start-version -->
```yaml
- name: Install cargoship
  env:
    VERSION: v0.42.0
  run: |
    set -euo pipefail
    curl -fsSLO "https://github.com/colonel-byte/cargoship/releases/download/${VERSION}/cargoship_Linux_x86_64.tar.gz"
    curl -fsSLO "https://github.com/colonel-byte/cargoship/releases/download/${VERSION}/checksums.txt"
    sha256sum --check --ignore-missing checksums.txt
    tar -xzf cargoship_Linux_x86_64.tar.gz cargoship
    install -m 0755 cargoship /usr/local/bin/cargoship
```
<!-- x-release-please-end-version -->

`checksums.txt` and a `.sigstore.json` bundle ship with every release, so this can go as far as the policy needs: the checksum above, and [a cosign verification](signing-packages.md) of the archive where a signature is required. The same pinning argument applies - the version is a literal here rather than a variable only because this snippet is also what a reader runs by hand.

**The container image, as the job's container.** Pinned by digest rather than by tag, and the binary is in the image rather than downloaded per run:

```yaml
jobs:
  render:
    runs-on: ubuntu-latest
    container:
      image: ghcr.io/colonel-byte/cargoship@sha256:<digest>
```

**`go install`, if the job already has Go.** Convenient and the least reproducible of the four, since it resolves and compiles from the module proxy on every run. Fine for a repository that is already a Go build.

## The Workflow

Complete, and safe to copy: `contents: read` at the top of the file, the write granted on the job that needs it, and every action pinned to a commit SHA.

```yaml
---
name: Render definitions

on:
  # Whenever an input changes, which is the trigger that matters: a template fix or a new
  # version file should reach the rendered tree in the same review cycle.
  push:
    branches: [main]
    paths:
      - templates/**
      - versions/**
  # Plus a floor, so a repository nobody has touched in a month is still known to be current.
  schedule:
    - cron: 41 6 * * 1
  workflow_dispatch:

permissions:
  contents: read

concurrency:
  group: render-definitions
  cancel-in-progress: false

jobs:
  render:
    runs-on: ubuntu-latest
    permissions:
      contents: write
      pull-requests: write
    env:
      BRANCH: chore/render-definitions
    steps:
      - name: Checkout
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1

      # Pinned through a repository variable rather than left to resolve latest, so a render
      # that surprises someone says which build produced it. Swap this step for the verified
      # curl form above where the download has to be checked.
      - name: Setup cargoship
        uses: colonel-byte/setup-cargoship@4da2adc72270551216e4c58daae04ea8e3391948 # v1.0.0
        with:
          version: ${{ vars.CARGOSHIP_VERSION }}

      # One render per values file, so adding a version is adding a file. The rendered
      # directory is removed first: a template that stops writing a file should stop
      # carrying it, and an overwrite alone would leave the old one behind.
      - name: Render
        run: |
          set -euo pipefail

          for input in versions/*.yaml; do
            name=$(basename "$input" .yaml)
            if [ "$name" = common ]; then
              continue
            fi

            rm -rf "packages/$name"
            cargoship dev render \
              --template templates/distro.yaml.tmpl \
              --template templates/values.yaml.tmpl \
              --values versions/common.yaml \
              --values "$input" \
              --out "packages/$name"
          done

      - name: Stage what moved
        id: changes
        run: |
          set -euo pipefail

          git add --all -- packages

          if git diff --cached --quiet; then
            echo "changed=false" >> "$GITHUB_OUTPUT"
            echo "Nothing moved since the last render." >> "$GITHUB_STEP_SUMMARY"
            exit 0
          fi

          echo "changed=true" >> "$GITHUB_OUTPUT"
          git diff --cached --stat | tail -n 20

      - name: Push the branch
        if: steps.changes.outputs.changed == 'true'
        run: |
          set -euo pipefail

          git config user.name "render-bot"
          git config user.email "render-bot@users.noreply.github.com"

          git switch --force-create "$BRANCH"
          git commit --message "chore: re-render definitions"
          git push --force origin "$BRANCH"

      - name: Open or update the pull request
        if: steps.changes.outputs.changed == 'true'
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
          REPO: ${{ github.repository }}
        run: |
          set -euo pipefail

          number=$(
            gh pr list --repo "$REPO" --head "$BRANCH" --state open \
              --limit 1 --json number --jq '.[].number'
          )
          if [ -z "$number" ]; then
            gh pr create --repo "$REPO" --base main --head "$BRANCH" \
              --title "chore: re-render definitions" \
              --body "Rendered by \`.github/workflows/render.yaml\`. Do not edit these files on this branch -- the next run force-pushes over them. Change the template or the values file instead."
          fi
```

The render itself needs no credentials and reaches no network: `dev render` reads its templates and values files and nothing else. Everything above it in the job is about getting the binary, and everything below it is about the pull request.

## What `dev render` Already Checks, and What to Add

A rendered `distro.yaml` is validated against the package schema before it is written, so the render step above fails the job on a definition that could not be built. That covers the definition and nothing else, so two cheap steps are worth adding after it:

```yaml
- name: Validate the rendered tree
  run: cargoship validate packages/*/distro.yaml

- name: Check each inventory against the package it installs
  run: cargoship validate inventories/production.yaml --package packages/1.37.1-k3s1
```

The first is redundant with the render for a definition written by this run and is not redundant for one a hand-edit reached - it is what catches the committed tree having been edited directly. The second is the one that earns its place. `spec.config.values` is untyped in the inventory schema, because its shape belongs to whichever package is being installed, so an inventory overriding a value the package does not declare validates cleanly on its own and fails at install time. `--package` takes a package source directory, so the check runs against the definition this workflow just rendered, before the package exists.

What **not** to put in this workflow:

| Don't                                   | Why                                                                                                                                                                                              |
| --------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `cargoship create` on every render      | A build fetches every image and file the definition declares, which is gigabytes per package and the thing a hosted runner's disk runs out of. Build on the release path, not on the render path |
| `--no-validate`                         | The flag is for a template under active edit at a terminal. In CI it turns the one check this workflow gets for free back off                                                                    |
| A publish step                          | Rendering and publishing have different blast radii and different credentials. Keep the push to a registry on the release workflow, reading whatever is on the default branch                    |
| A commit straight to the default branch | See the table at the top: an unreviewed render is an unreviewed fleet change                                                                                                                     |

## The Token, and Why the Pull Request May Look Inert

A pull request opened with `GITHUB_TOKEN` **does not trigger the repository's own `pull_request` workflows**. The render lands with a green tick and no checks, and a branch protection rule requiring a check nothing started cannot be satisfied.

The workflow above accepts that, which is fine when the render has no other CI to start. Where the definitions repository does have checks that must run on the render, open the pull request as a GitHub App instead - a token minted per run, scoped to this repository, which counts as a different actor:

```yaml
- name: Get app token
  id: app-token
  uses: actions/create-github-app-token@64ab729a6294000f5aa4a4bc0b73d1f32bc45510 # v3.3.0
  with:
    client-id: ${{ secrets.RENDER_APP_ID }}
    private-key: ${{ secrets.RENDER_APP_KEY }}

- name: Checkout
  uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
  with:
    token: ${{ steps.app-token.outputs.token }}
```

Then pass `${{ steps.app-token.outputs.token }}` as `GH_TOKEN` to the `gh pr` step, and drop the job's `contents: write` and `pull-requests: write` - the app token carries those, and the workflow's own token no longer needs them. A personal access token works and is the thing to avoid: it ties the repository's automation to one person's account and expires on their schedule.

## Chasing an Upstream Release

The workflow above re-renders what the inputs say. Deciding that upstream has cut a release the inputs do not mention yet is a separate job, and it is the half that cannot be copied from here, because what counts as "a release worth rendering" is a policy: a minor line you track, a channel, a revision suffix, a release that has to have published its RPMs before the definition is installable.

The pattern that works is a pinned input file plus one step that moves it, kept apart from the render:

1. **A pin file lists what is tracked** - a minor line, and the tag currently rendered for it. This repository's is [`thirdparty-src/pins.json`](https://github.com/colonel-byte/cargoship/blob/main/thirdparty-src/pins.json), moved by `mage generate:updatePins` and checked against upstream weekly by [`check-pins.yaml`](../workflows/check-pins.md).
2. **One step resolves the newest acceptable tag** for each tracked line - `gh release list --repo rancher/rke2`, filtered by the policy - and writes a new file under `versions/` if it moved.
3. **The render runs after it**, in the same job, so one pull request carries the new input and the definitions it produced. A reviewer then sees the version bump and its rendered consequence together, which is the whole value of the arrangement.

Two things upstream does that will bite a workflow that skips step 2's filtering: a release whose assets are **re-cut in place** keeps its URL while its digests change, and a revision upstream **retires** keeps its git tag long after the packages the definition installs have gone. The first produces a shasum mismatch at build time, the second a definition that renders and cannot be installed. Checking that what a release publishes is still fetchable *before* writing its values file is what keeps the pull request honest; this repository's generator probes for exactly that, and skips rather than renders.

## Scorecard-Safe by Default

If the definitions repository publishes an [OpenSSF Scorecard](https://securityscorecards.dev), the workflow above is already written to its rules. Keep them when editing it:

- **`permissions: contents: read` at the top of the file**, with each write granted on the job that needs it. A top-level `contents: write` takes Token-Permissions to zero; the same grant on a job costs nothing. A job-level block replaces the top-level one rather than merging, so a job that gains a write restates the reads it still uses.
- **Every `uses:` pinned to a full commit SHA**, with the tag in a trailing comment. The comment is what Dependabot reads to offer the bump, so keep it accurate when the SHA moves.
- **Untrusted values bound through `env:`**, never interpolated into a `run:` block. `${{ github.event.* }}` spliced into a shell is attacker-controlled text in a script.
- **No `ref:` taken from the event payload**, which is why the workflow above is triggered by `push` and `schedule` rather than by `pull_request_target`.

## Where to Go Next

- [Generating Definitions From a Template](generating-definitions.md) - the render itself: values merging, the available template functions, and what the schema check does and does not catch
- [`refresh-examples.yaml`](../workflows/refresh-examples.md) - this repository's own version of this workflow, over a matrix of distros, flavors and release tags
- [`check-pins.yaml`](../workflows/check-pins.md) - the other half: noticing that upstream moved without rendering anything
- [Signing Packages](signing-packages.md) - what to verify about the binary the runner just downloaded, and what to sign on the release path
