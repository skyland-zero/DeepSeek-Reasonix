$ErrorActionPreference = 'Stop'

$localOwned = @(
  'internal/cli/toolcard.go',
  'internal/cli/transcript.go',
  'internal/cli/md.go',
  'internal/cli/diffview.go',
  'internal/cli/status_footer.go',
  'internal/cli/theme.go',
  'internal/cli/banner.go',
  'internal/cli/castle.go',
  'internal/cli/md_table.go',
  'internal/cli/table_copy.go',
  'internal/cli/slash_quota.go',
  'internal/opencodego',
  'internal/control/plan_archive_local.go',
  'SKYCODE.md',
  'skycode.example.toml',
  'npm/skycode'
)

git fetch upstream
if ($LASTEXITCODE -ne 0) { throw 'git fetch upstream failed' }

$dirty = git status --porcelain | Where-Object { $_ -notmatch '^\?\?' }
if ($dirty) {
  throw 'Worktree has staged/unstaged changes; commit or stash before syncing.'
}

git merge --rerere-autoupdate upstream/main-v2
if ($LASTEXITCODE -ne 0) {
  $conflicts = git diff --name-only --diff-filter=U
  if ($conflicts) {
    Write-Host 'Conflicts remain. Resolve per MERGE_POLICY.md, then commit.'
    $conflicts | ForEach-Object { Write-Host "  $_" }
    exit 1
  }
}

go build ./...
if ($LASTEXITCODE -ne 0) { throw 'go build failed' }

go test ./internal/cli/ ./internal/control/ ./internal/agent/ -count=1
if ($LASTEXITCODE -ne 0) { throw 'targeted tests failed' }

$mb = git merge-base HEAD upstream/main-v2
Write-Host 'Upstream changes to local-owned files (review whether to port):'
$found = $false
foreach ($p in $localOwned) {
  $changed = git diff --name-only $mb upstream/main-v2 -- $p
  if ($changed) {
    $found = $true
    Write-Host "  $p"
  }
}
if (-not $found) {
  Write-Host '  (none)'
}

Write-Host 'Sync complete. Commit the merge when ready.'
