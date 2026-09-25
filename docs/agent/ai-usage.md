# AI usage in cargoship's development

This document discloses how AI coding agents are used to build cargoship. It is not a CycloneDX ML-BOM or SPDX AI profile — those describe AI/ML models and datasets shipped *inside* a product, and cargoship has none: it's an Ansible/Kubernetes deployment CLI with no inference or model-serving code of its own. What follows instead is a disclosure of the tools used to *write* cargoship, in the same spirit as the supply-chain artifacts described in [the security policy](../security.md) — an SBOM discloses what's in a release; this discloses how the release was produced.

## Tools in use

Claude Code (Anthropic, Claude models) is used across the repository for code changes, documentation, and the design records under this directory. Its operating rules for this repository live in the various `AGENTS.md` files scattered through the tree — [the repository root's](../../AGENTS.md), and the per-directory ones it points to — which tell an agent how this codebase expects to be worked in, not just what the code does.

## Human review

AI-authored and human-authored changes go through the same gate: a reviewed pull request, and the same CI checks — CodeQL, linting, the test suites, OpenSSF Scorecard — regardless of who or what wrote the diff. Nothing here grants an AI-authored change an exemption from review or from CI.

## Where the provenance trail lives

Two records carry this forward:

- [`choice-*.md`](.) files in this directory record non-obvious design decisions and the tradeoffs behind them, agent-driven or not — see [`AGENTS.md`](../../AGENTS.md) for why they're treated as constraints rather than history.
- Every tagged release publishes `ai-bom.json` as a release asset, attested the same way as the release's [SBOM and provenance](../security.md#verifying-a-release). It's a per-release snapshot of this document's claims — which tools, what they're used for, and the human-review statement above — rather than a list of what's in that particular release.
