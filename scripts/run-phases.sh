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
  local log_file="$LOG_DIR/phase-${phase}-${name}.log"
  local silent_failure_log="$LOG_DIR/phase-${phase}-${name}-silent-failures.log"
  local code_review_log="$LOG_DIR/phase-${phase}-${name}-code-review.log"
  local security_log="$LOG_DIR/phase-${phase}-${name}-security.log"

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

  echo "Running Claude session... (log: $log_file)"
  echo ""

  # Run Claude non-interactively with the plan as context
  claude --model claude-sonnet-4-6 -p "$(cat <<PROMPT
You are implementing Phase ${phase} of the Cloudzilla project.

Read and implement the plan at: docs/superpowers/plans/${plan_file}

You are already on branch: ${branch}

Instructions:
1. Use the superpowers:executing-plans skill to implement the plan task-by-task.
2. Follow all conventions in CLAUDE.md exactly (PostgreSQL syntax, Templ components, Store→Service→Handler layering).
3. After completing the implementation, run: go build ./... to verify it compiles.
4. Commit all changes with message format: feat(phase-${phase}): <description>
5. Do NOT merge or push — leave the branch ready for review.

IMPORTANT: Do not ask clarifying questions. Follow the plan as written.
PROMPT
)" 2>&1 | tee "$log_file"

  local impl_exit="${PIPESTATUS[0]}"

  if [[ $impl_exit -ne 0 ]]; then
    echo ""
    echo "✗ Phase ${phase} FAILED (exit $impl_exit). Branch left at: ${branch}"
    echo "  Check log: $log_file"
    git checkout main
    return 1
  fi

  # --- Review 1: Silent failure hunter ---
  echo ""
  echo "Review 1/3: Silent failure hunter... (log: $silent_failure_log)"
  echo ""

  claude --model claude-sonnet-4-6 -p "$(cat <<PROMPT
Use the pr-review-toolkit:silent-failure-hunter skill to review the Phase ${phase} (${name}) implementation.

The changes are on branch: ${branch}
Run: git diff main...HEAD to see what was added.

Focus on:
- Silent failures and swallowed errors
- Missing error returns or unchecked error values
- Unhandled edge cases that could cause data loss or incorrect state

Report high-confidence findings only. Be concise.
PROMPT
)" 2>&1 | tee "$silent_failure_log"

  # --- Review 2: Code reviewer ---
  echo ""
  echo "Review 2/3: Code reviewer... (log: $code_review_log)"
  echo ""

  claude --model claude-sonnet-4-6 -p "$(cat <<PROMPT
Use the pr-review-toolkit:code-reviewer skill to review the Phase ${phase} (${name}) implementation.

The changes are on branch: ${branch}
Run: git diff main...HEAD to see what was added.

Focus on:
- Bugs and logic errors
- Code quality and maintainability
- Adherence to CLAUDE.md conventions (Store→Service→Handler layering, PostgreSQL syntax, Templ patterns)
- Missing tests or validation

Report high-confidence findings only. Be concise.
PROMPT
)" 2>&1 | tee "$code_review_log"

  # --- Review 3: Security review ---
  echo ""
  echo "Review 3/3: Security review... (log: $security_log)"
  echo ""

  claude --model claude-sonnet-4-6 -p "$(cat <<PROMPT
Perform a targeted security review of the Phase ${phase} (${name}) implementation.

The changes are on branch: ${branch}
Run: git diff main...HEAD to see what was added.

Focus exclusively on security issues:
- Authentication and authorization bypasses
- SQL injection (missing parameterization, raw query construction)
- XSS (unescaped output in Templ templates — note: Templ auto-escapes, but check templ.Raw() usage)
- CSRF (state-mutating endpoints without protection)
- Insecure direct object references (missing ownership checks)
- Sensitive data exposure (tokens, keys, PII in logs or responses)
- Input validation gaps at system boundaries

Report high-confidence findings only. Be concise.
PROMPT
)" 2>&1 | tee "$security_log"

  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "  Phase ${phase} ready for review"
  echo "  Branch:            ${branch}"
  echo "  Impl log:          ${log_file}"
  echo "  Silent failures:   ${silent_failure_log}"
  echo "  Code review:       ${code_review_log}"
  echo "  Security review:   ${security_log}"
  echo ""
  echo "  Next steps:"
  echo "  1. Fix any high-confidence findings from the reviews above"
  echo "  2. Create PR:  gh pr create --base main --head ${branch}"
  echo "  3. Review and merge the PR"
  echo "  4. Press Enter here to continue to the next phase"
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
