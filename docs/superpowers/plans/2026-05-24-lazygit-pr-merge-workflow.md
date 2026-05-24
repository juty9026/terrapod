# Lazygit PR Merge Workflow Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a lazygit custom command that merges the selected branch's GitHub PR, with a squash path that opens an editable PR-title/description commit-message template.

**Architecture:** Keep lazygit responsible only for the menu and selected branch handoff, and put merge behavior in a focused POSIX shell helper. Test the helper by stubbing `gh` and `$EDITOR`, and test the managed lazygit config by rendering it with chezmoi.

**Tech Stack:** chezmoi source layout, lazygit `customCommands`, GitHub CLI `gh pr merge`, POSIX shell tests.

---

### Task 1: Lazygit PR Merge Custom Command And Helper

**Files:**
- Modify: `dot_config/lazygit/config.yml`
- Create: `dot_config/lazygit/scripts/executable_merge-pr`
- Create: `tests/lazygit_pr_merge_test.sh`

- [ ] **Step 1: Write the failing helper/config test**

Create `tests/lazygit_pr_merge_test.sh` with a shell test that:

```sh
#!/bin/sh
set -eu

repo_root="$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

fail() {
  printf '%s\n' "not ok - $1" >&2
  exit 1
}

pass() {
  printf '%s\n' "ok - $1"
}

write_stub() {
  path="$1"
  shift
  {
    printf '%s\n' '#!/bin/sh'
    printf '%s\n' "$@"
  } >"$path"
  chmod +x "$path"
}

assert_contains() {
  haystack="$1"
  needle="$2"
  message="$3"

  if ! printf '%s\n' "$haystack" | grep -F "$needle" >/dev/null; then
    fail "$message"
  fi

  pass "$message"
}

mkdir -p "$tmp_dir/bin" "$tmp_dir/home" "$tmp_dir/editors"

rendered_config="$(
  chezmoi \
    --source "$repo_root" \
    --destination "$tmp_dir/home" \
    cat "$tmp_dir/home/.config/lazygit/config.yml"
)"

assert_contains "$rendered_config" "description: Merge GitHub PR..." "lazygit config exposes the PR merge command as a follow-up flow"
assert_contains "$rendered_config" "title: Merge pull request" "lazygit config prompts for merge strategy"
assert_contains "$rendered_config" "Merge commit" "lazygit config offers merge commit strategy"
assert_contains "$rendered_config" "Squash" "lazygit config offers squash strategy"
assert_contains "$rendered_config" "merge-pr" "lazygit config delegates PR merge behavior to helper"

helper="$repo_root/dot_config/lazygit/scripts/executable_merge-pr"

write_stub "$tmp_dir/bin/gh" \
  'printf "%s\n" "$*" >>"$GH_CALL_LOG"' \
  'if [ "$1" = "pr" ] && [ "$2" = "view" ]; then' \
  '  printf "%s\n" "Implement lazygit PR merge"' \
  '  printf "%s\n" "This PR adds a merge menu."' \
  '  exit 0' \
  'fi' \
  'if [ "$1" = "pr" ] && [ "$2" = "merge" ]; then' \
  '  i=1' \
  '  for arg in "$@"; do' \
  '    if [ "$arg" = "--body-file" ]; then' \
  '      eval "body_file=\\${$((i + 1))}"' \
  '      cat "$body_file" >"$GH_BODY_CAPTURE"' \
  '    fi' \
  '    i=$((i + 1))' \
  '  done' \
  '  exit 0' \
  'fi' \
  'exit 64'

write_stub "$tmp_dir/editors/append-editor" \
  'printf "%s\n" "Edited body line." >>"$1"'

export PATH="$tmp_dir/bin:/usr/bin:/bin"
export GH_CALL_LOG="$tmp_dir/gh-calls.log"
export GH_BODY_CAPTURE="$tmp_dir/body.txt"
export EDITOR="$tmp_dir/editors/append-editor"

sh "$helper" feature/pr-menu merge

if ! grep -F "pr merge feature/pr-menu --merge" "$GH_CALL_LOG" >/dev/null; then
  fail "merge strategy should call gh pr merge with --merge"
fi

pass "merge strategy calls gh pr merge with --merge"

: >"$GH_CALL_LOG"
sh "$helper" feature/pr-menu squash

if ! grep -F "pr view feature/pr-menu --json title,body --jq .title, .body" "$GH_CALL_LOG" >/dev/null; then
  fail "squash strategy should read PR title and body"
fi

pass "squash strategy reads PR title and body"

if ! grep -F "pr merge feature/pr-menu --squash --subject Implement lazygit PR merge --body-file" "$GH_CALL_LOG" >/dev/null; then
  fail "squash strategy should pass edited title as subject"
fi

pass "squash strategy passes edited title as subject"

if ! grep -F "This PR adds a merge menu." "$GH_BODY_CAPTURE" >/dev/null; then
  fail "squash strategy should pass PR description as body"
fi

if ! grep -F "Edited body line." "$GH_BODY_CAPTURE" >/dev/null; then
  fail "squash strategy should include editor changes in body"
fi

pass "squash strategy passes edited description as body"

write_stub "$tmp_dir/editors/remove-title-editor" \
  'sed "/^Title:/d" "$1" >"$1.tmp"' \
  'mv "$1.tmp" "$1"'

export EDITOR="$tmp_dir/editors/remove-title-editor"

if sh "$helper" feature/pr-menu squash >"$tmp_dir/missing-title.out" 2>"$tmp_dir/missing-title.err"; then
  fail "squash strategy should reject an edited message without a title"
fi

if ! grep -F "missing a Title" "$tmp_dir/missing-title.err" >/dev/null; then
  fail "squash strategy should explain missing title errors"
fi

pass "squash strategy rejects missing title"
```

- [ ] **Step 2: Run the new test to verify it fails**

Run:

```bash
sh tests/lazygit_pr_merge_test.sh
```

Expected: FAIL because `dot_config/lazygit/scripts/executable_merge-pr` does not exist and the lazygit config has no merge custom command.

- [ ] **Step 3: Implement the lazygit custom command**

Update `dot_config/lazygit/config.yml` so it keeps the existing pager settings and adds:

```yaml
customCommands:
  - key: "X"
    context: "localBranches"
    description: "Merge GitHub PR..."
    prompts:
      - type: "menu"
        title: "Merge pull request"
        key: "MergeStrategy"
        options:
          - name: "Merge commit"
            description: "Create a merge commit"
            value: "merge"
          - name: "Squash"
            description: "Squash commits into one commit"
            value: "squash"
      - type: "confirm"
        title: "Merge PR?"
        body: "Merge the GitHub PR for the selected branch?"
    command: "~/.config/lazygit/scripts/merge-pr {{.SelectedLocalBranch.Name | quote}} {{.Form.MergeStrategy | quote}}"
    output: "terminal"
    loadingText: "Merging PR..."
```

- [ ] **Step 4: Implement the helper**

Create `dot_config/lazygit/scripts/executable_merge-pr` as a POSIX shell script with this behavior:

```sh
#!/bin/sh
set -eu

fail() {
  printf '%s\n' "merge-pr: $1" >&2
  exit 1
}

branch="${1:-}"
strategy="${2:-}"

[ -n "$branch" ] || fail "branch is required"
[ -n "$strategy" ] || fail "merge strategy is required"

editor="${EDITOR:-vi}"

case "$strategy" in
  merge)
    exec gh pr merge "$branch" --merge
    ;;
  squash)
    ;;
  *)
    fail "unknown merge strategy: $strategy"
    ;;
esac

message_file="$(mktemp "${TMPDIR:-/tmp}/merge-pr-message.XXXXXX")"
body_file="$(mktemp "${TMPDIR:-/tmp}/merge-pr-body.XXXXXX")"

cleanup() {
  rm -f "$message_file" "$body_file"
}
trap cleanup EXIT INT TERM

pr_data="$(gh pr view "$branch" --json title,body --jq '.title, .body')"
pr_title="$(printf '%s\n' "$pr_data" | sed -n '1p')"
pr_body="$(printf '%s\n' "$pr_data" | sed '1d')"

{
  printf '%s\n' '---'
  printf 'Title: %s\n' "$pr_title"
  printf '%s\n' '---'
  printf '\n'
  printf '%s\n' "$pr_body"
} >"$message_file"

"$editor" "$message_file"

title="$(
  sed -n 's/^Title:[[:space:]]*//p' "$message_file" |
    sed -n '1p'
)"

[ -n "$title" ] || fail "edited squash message is missing a Title"

awk '
  BEGIN { frontmatter = 0; body = 0 }
  /^---$/ {
    frontmatter++
    if (frontmatter == 2) {
      body = 1
      next
    }
  }
  body {
    print
  }
' "$message_file" |
  sed '1{/^$/d;}' >"$body_file"

exec gh pr merge "$branch" --squash --subject "$title" --body-file "$body_file"
```

- [ ] **Step 5: Run the new test to verify it passes**

Run:

```bash
sh tests/lazygit_pr_merge_test.sh
```

Expected: PASS for config rendering, merge strategy, squash strategy, body parsing, and missing-title rejection.

- [ ] **Step 6: Run the full local shell test suite**

Run:

```bash
for test_file in tests/*.sh; do sh "$test_file"; done
for test_file in tests/*.zsh; do zsh "$test_file"; done
```

Expected: all tests print `ok - ...` lines and the command exits 0.

- [ ] **Step 7: Commit**

Run:

```bash
git add dot_config/lazygit/config.yml dot_config/lazygit/scripts/executable_merge-pr tests/lazygit_pr_merge_test.sh docs/superpowers/plans/2026-05-24-lazygit-pr-merge-workflow.md
git commit -m "feat: add lazygit PR merge workflow"
```

Expected: one commit containing the lazygit command, helper, tests, and implementation plan.
