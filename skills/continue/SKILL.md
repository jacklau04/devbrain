---
name: continue
description: |
  devbrain resume cursor (Stage C — Assemble). Pulls the latest brain for the
  current project, refreshes the live world (git/issues/CI), and produces a small
  briefing so you can pick up where you (or another machine) left off. Use when
  asked to "continue", "resume", "where was I", "pick up where I left off", or
  "what's the state of this".
---

# /continue — assemble the right amount of context

You are resuming work. devbrain's job here is **subtraction, not stuffing**: pull
only what's relevant from the brain, refresh what's live, and hand back a short
briefing with pointers. The raw log is the source of truth; the brain is a
queryable projection of it.

## Steps

### 1. Resolve identity (mechanical, from the working repo)
```bash
cwd="$(pwd)"
remote="$(git -C "$cwd" remote get-url origin 2>/dev/null)"
if [ -n "$remote" ]; then project="$(basename "${remote%.git}")"; else project="$(basename "$cwd")"; fi
project="$(printf '%s' "$project" | tr '[:upper:] ' '[:lower:]-' | tr -cd '[:alnum:]._-')"
branch="$(git -C "$cwd" branch --show-current 2>/dev/null)"
echo "project=$project branch=$branch"
```

### 2. Sync the brain data, then refresh the index
Pull the latest logs/pages other machines pushed, then re-load this project's
pages into gbrain so search is current.
```bash
DATA="${DEVBRAIN_DATA:-$HOME/devbrain-data}"
git -C "$DATA" pull --rebase --autostash --quiet 2>/dev/null || true
DEVBRAIN_DATA="$DATA" ~/.claude/hooks/devbrain-rebuild.sh 2>/dev/null \
  || (command -v gbrain >/dev/null && for f in "$DATA"/projects/"$project"/brain/*.md; do [ -e "$f" ] && gbrain put "project/$(basename "$f" .md)" < "$f" >/dev/null 2>&1; done)
```

### 3. Query the brain
Use `gbrain search` (reliable across embedders). `gbrain query --detail low`
needs a "compiled truth" layer that is not built yet, so prefer search.
```bash
gbrain search "$project ${branch:-overview}" 2>/dev/null | head -20
# Then read the top 1-3 pages in full for the actual content:
#   gbrain get "project/<slug>"
```

### 4. Refresh the live world
Status lives in the world, never invented.
```bash
git -C "$cwd" fetch --quiet 2>/dev/null || true
git -C "$cwd" status -sb | head -20
git -C "$cwd" log --oneline -5
command -v gh >/dev/null && gh issue list --limit 10 2>/dev/null || true
command -v gh >/dev/null && gh pr status 2>/dev/null || true
```

### 5. Brief the user (short)
Produce, in a few lines:
- **Where you are:** project, branch, and the task the branch implies.
- **From the brain:** the 2-4 most relevant facts/decisions/open items (with the
  page slug as a pointer, e.g. `project/devbrain-decisions`).
- **From the world:** uncommitted changes, ahead/behind, open issues/PRs, CI.
- **Suggested next action**, one line.

Keep it to a briefing plus pointers. Do not dump whole pages — link them. If the
brain has nothing for this project, say so and offer to start capturing (the
capture hook may not be installed, or this is a new project).
