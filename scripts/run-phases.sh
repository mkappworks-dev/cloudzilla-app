#!/usr/bin/env bash
# run-phases.sh — Run each Cloudzilla phase plan in a separate Claude session.
# Usage:
#   ./scripts/run-phases.sh              # Run all phases from 8.2 onward
#   ./scripts/run-phases.sh 9.1         # Start from a specific phase
#   ./scripts/run-phases.sh 9.1 10.3   # Run a range (inclusive)
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PLANS_DIR="$REPO_ROOT/docs/superpowers/plans"
LOG_DIR="$REPO_ROOT/scripts/logs"
mkdir -p "$LOG_DIR"

# Ordered list of phases to implement (8.2 onward; 8.1 already done)
PHASES=(
  "8.2:feat:audit-log:2026-03-25-phase-8.2-audit-log.md"
  "8.3:tech:sso:2026-03-25-phase-8.3-sso.md"
  "9.1:feat:project-boards:2026-03-25-phase-9.1-project-boards.md"
  "9.2:feat:wiki:2026-03-25-phase-9.2-wiki.md"
  "9.3:feat:issue-pinning-locking:2026-03-25-phase-9.3-issue-pinning-locking.md"
  "10.1:feat:insights:2026-03-25-phase-10.1-insights.md"
  "10.2:feat:mentions:2026-03-25-phase-10.2-mentions.md"
  "10.3:feat:saved-replies:2026-03-25-phase-10.3-saved-replies.md"
  "11.1:feat:email-notifications:2026-03-25-phase-11.1-email-notifications.md"
  "11.2:feat:oauth-apps:2026-03-25-phase-11.2-oauth-apps.md"
  "11.3:feat:webhook-improvements:2026-03-25-phase-11.3-webhook-improvements.md"
  "12.1:feat:watching:2026-03-25-phase-12.1-watching.md"
  "12.2:feat:activity-feed:2026-03-25-phase-12.2-activity-feed.md"
  "12.3:feat:discussions:2026-03-25-phase-12.3-discussions.md"
  "13.1:feat:gists:2026-03-25-phase-13.1-gists.md"
  "13.2:feat:profile-readme:2026-03-25-phase-13.2-profile-readme.md"
  "13.3:feat:repo-topics:2026-03-25-phase-13.3-repo-topics.md"
  "14.1:feat:private-issues:2026-03-25-phase-14.1-private-issues.md"
  "14.2:feat:archive-templates:2026-03-25-phase-14.2-archive-templates.md"
  "14.3:feat:soft-delete:2026-03-25-phase-14.3-soft-delete.md"
  "15.1:feat:advanced-code-search:2026-03-25-phase-15.1-advanced-code-search.md"
  "15.2:feat:explore-trending:2026-03-25-phase-15.2-explore-trending.md"
  "15.3:feat:dependency-graph:2026-03-25-phase-15.3-dependency-graph.md"
)

# Parse optional start/end args
START_PHASE="${1:-8.2}"
END_PHASE="${2:-15.3}"

# Compare phase numbers (handles X.Y format)
phase_gte() { awk "BEGIN{exit !($1 >= $2)}"; }
phase_lte() { awk "BEGIN{exit !($1 <= $2)}"; }

# Print the next phase number after the given one, for display purposes
next_phase() {
  local current="$1"
  local found=false
  for entry in "${PHASES[@]}"; do
    IFS=':' read -r p _ _ _ <<< "$entry"
    if $found; then echo "$p"; return; fi
    [[ "$p" == "$current" ]] && found=true
  done
  echo "complete"
}

run_phase() {
  local phase="$1" type="$2" name="$3" plan_file="$4"
  local branch="${type}/phase-${phase}-${name}"
  local plan_path="$PLANS_DIR/$plan_file"
  local prompts_dir="$REPO_ROOT/scripts/prompts"
  mkdir -p "$prompts_dir"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Phase ${phase}: ${name}"
  echo "  Branch: ${branch}"
  echo "  Plan:   ${plan_file}"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

  if [[ ! -f "$plan_path" ]]; then
    echo "ERROR: Plan file not found: $plan_path"
    exit 1
  fi

  # Ensure we're on main and up to date before branching
  cd "$REPO_ROOT"
  git checkout main
  git pull --ff-only 2>/dev/null || true

  # Create branch (skip if already exists)
  if git show-ref --verify --quiet "refs/heads/$branch"; then
    echo "Branch '$branch' already exists — checking it out."
    git checkout "$branch"
  else
    git checkout -b "$branch"
  fi

  # --- Session 1: Implementation ---
  cat > "$prompts_dir/phase-${phase}-${name}-impl.md" <<PROMPT
You are implementing Phase ${phase} of the Cloudzilla project.

Read and implement the plan at: docs/superpowers/plans/${plan_file}

You are already on branch: ${branch}

Instructions:
1. Use the superpowers:executing-plans skill to implement the plan task-by-task.
2. Follow all conventions in CLAUDE.md exactly (PostgreSQL syntax, Templ components, Store→Service→Handler layering).
3. Write Go tests for any new logic, service methods, or security-sensitive code where testing adds value. Not everything needs a test — use judgement.
4. Run: go test ./... and go build ./... to verify everything passes and compiles.
5. Commit all changes with message format: feat(phase-${phase}): <description>
6. Do NOT merge or push — leave the branch ready for review.

IMPORTANT: Do not ask clarifying questions. Follow the plan as written.
PROMPT

  echo ""
  echo "  Step 1/2 — Implementation"
  echo "  When Claude opens, type:"
  echo "  @scripts/prompts/phase-${phase}-${name}-impl.md"
  echo ""
  read -r -p "  Press Enter to open Claude... "
  claude --model claude-sonnet-4-6

  # --- Session 2: Review + docs + PR summary ---
  cat > "$prompts_dir/phase-${phase}-${name}-review.md" <<PROMPT
You are doing post-implementation work for Phase ${phase} (${name}). Complete all four steps below in order.

The changes are on branch: ${branch}
Run: git diff main...HEAD to see what was added.

## Step 1 — Silent failures
Check for:
- Swallowed errors or missing error returns
- Unchecked error values (e.g. \`_ = someErr\`)
- Unhandled edge cases that could cause data loss or incorrect state

## Step 2 — Code quality
Check for:
- Bugs and logic errors
- Adherence to CLAUDE.md conventions (Store→Service→Handler layering, PostgreSQL syntax, Templ patterns)
- Missing input validation or tests for security-sensitive code

## Step 3 — Security
Check for:
- Authentication and authorization bypasses
- SQL injection (missing parameterization, raw query construction)
- XSS (unescaped output in Templ — Templ auto-escapes, but check templ.Raw() usage)
- Insecure direct object references (missing ownership checks)
- Sensitive data exposure (tokens, keys, PII in logs or responses)

Fix all high-confidence findings from steps 1–3, then commit with message: fix(phase-${phase}): address review findings

## Step 4 — Docs update
Check and update the following files if they are outdated or incomplete:
- README.md — update feature list, setup instructions, or environment variables if new ones were added
- CLAUDE.md — update architecture notes, conventions, or the phase status table (mark phase ${phase} as Done)
- docs/roadmap.md — mark phase ${phase} as completed

Only make changes that are factually necessary. If a file is already accurate, leave it unchanged.
Commit any documentation changes with message: docs(phase-${phase}): update readme, claude.md, and roadmap

## Step 5 — PR summary
Write a PR summary to pr-summary.md at the repo root. Use the Write tool — do not print it in chat.

The file must contain exactly these two sections:

## Summary
Bullet points covering what was built: new tables, new service/store methods, new routes, new UI pages. Be specific — mention migration numbers, table names, route paths, and key design decisions.

## Security fixes
Bullet points for any security hardening done during implementation or review: authorization checks, scoped queries, input validation, error handling. Omit this section entirely if there were no security fixes.

Rules for pr-summary.md:
- Plain prose bullet points only — no code blocks, no markdown tables, no headings inside sections
- Name symbols inline with backticks: \`ErrProjectNotFound\`, \`CanRead\`, \`/projects/{id}\`
- Each bullet is one sentence, self-contained, specific enough to appear in a changelog
PROMPT

  echo ""
  echo "  Step 2/2 — Review, docs, and PR summary"
  echo "  When Claude opens, type:"
  echo "  @scripts/prompts/phase-${phase}-${name}-review.md"
  echo ""
  read -r -p "  Press Enter to open Claude... "
  claude --model claude-sonnet-4-6

  # Clean up prompt files
  rm -f "$prompts_dir/phase-${phase}-${name}-"*.md

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Phase ${phase} — Branch Summary"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  echo "  Commits:"
  git log main..HEAD --oneline | sed 's/^/    /'
  echo ""
  echo "  Files changed:"
  git diff --stat main...HEAD | sed 's/^/    /'
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Phase ${phase} ready for review"
  echo "  Branch: ${branch}"
  echo ""
  echo "  Next steps:"
  echo "  1. Fix any high-confidence findings from the reviews above"
  echo "  2. Press Enter here to continue to the next phase"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  read -r -p "  → Press Enter to continue to phase $(next_phase "$phase")... "

  # Pull merged main before starting next phase
  git checkout main
  git pull --ff-only 2>/dev/null || git pull
}

echo "Cloudzilla Phase Runner"
echo "Running phases: ${START_PHASE} → ${END_PHASE}"

for entry in "${PHASES[@]}"; do
  IFS=':' read -r phase type name plan_file <<< "$entry"

  # Skip phases outside the requested range
  phase_gte "$phase" "$START_PHASE" || continue
  phase_lte "$phase" "$END_PHASE"   || break

  run_phase "$phase" "$type" "$name" "$plan_file" || {
    echo ""
    echo "Stopped at phase ${phase}. Fix issues and re-run with:"
    echo "  ./scripts/run-phases.sh ${phase} ${END_PHASE}"
    exit 1
  }
done

echo ""
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  All phases complete!"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
