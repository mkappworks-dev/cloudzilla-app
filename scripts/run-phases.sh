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

run_phase() {
  local phase="$1" type="$2" name="$3" plan_file="$4"
  local branch="${type}/phase-${phase}-${name}"
  local plan_path="$PLANS_DIR/$plan_file"
  local log_file="$LOG_DIR/phase-${phase}-${name}.log"

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

  local exit_code="${PIPESTATUS[0]}"

  if [[ $exit_code -eq 0 ]]; then
    echo ""
    echo "✓ Phase ${phase} completed. Branch: ${branch}"
    echo "  Merging to main..."
    git checkout main
    git merge --ff-only "$branch" || {
      echo "  FF merge failed — merging with commit."
      git merge --no-ff "$branch" -m "feat: merge phase-${phase}-${name}"
    }
    echo "  ✓ Merged."
  else
    echo ""
    echo "✗ Phase ${phase} FAILED (exit $exit_code). Branch left at: ${branch}"
    echo "  Check log: $log_file"
    git checkout main
    return 1
  fi
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
