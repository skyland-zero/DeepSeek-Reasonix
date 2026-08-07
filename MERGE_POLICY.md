# Skycode fork merge policy

This fork keeps upstream `main-v2` as the host and isolates the local UI in
local-owned files so upstream merges stay small and predictable.

## File ownership

Local-owned (merge=keep-ours — freely editable, upstream changes are reviewed
manually via the sync script's report):

- `internal/cli/toolcard.go`, `transcript.go`, `md.go`, `diffview.go`,
  `status_footer.go`, `theme.go`, `banner.go`, `castle.go`, `md_table.go`,
  `table_copy.go`, `slash_quota.go`
- `internal/opencodego/**`, `internal/control/plan_archive_local.go`
- Fork docs/packaging: `SKYCODE.md`, `skycode.example.toml`, `npm/skycode/**`
- Local renderer tests that replaced upstream expectations:
  `chat_render_test.go`, `chat_tui_test.go`, `transcript_test.go`,
  `md_test.go`, `diffview_test.go`, `toolcard_test.go`,
  `status_footer_test.go`, `theme_test.go`, `banner_test.go`,
  `md_table_test.go`, `table_copy_test.go`, `slash_quota_test.go`,
  `statusline_test.go`, `consecutive_tool_markers_test.go`,
  `math_e2e_test.go`, `fixed_wrap_test.go`

Upstream-owned with small documented hooks:

- `internal/cli/chat_tui.go` — hooks: `renderTUIBanner` method lives in
  `banner.go`; tool-card state fields (`toolCardIdx`) and completion marks in
  the ToolDispatch/ToolResult cases; `statusModeTag` in `status_footer.go`;
  `beginToolRunning(id, name)`, `streamAnswer`, `collapseToolOutput`,
  `reasoningBlock`, `renderUserBubble` live in `transcript.go`; plan-mode
  toggle + quit messages in the key/slash handlers; viewport re-anchor in
  `Update`.
- `internal/config/config.go` — `ShowTurnReceipt` UI field + `DefaultSystemPrompt`
  identity injection.
- `internal/i18n/*` — fork keys: `Subtitle`, `PlanModeToggleBusy`, quota keys;
  `RecoveryPaused` branded to Skycode.
- `internal/agent`, `internal/control` — non-blocking plan mode and ask-tool
  relaxation are marked with `[MODIFIED]` comments.

Everything else is upstream-owned: do not edit it in local work unless the
change is a documented hook.

## Sync workflow

1. `git fetch upstream`
2. `git merge --rerere-autoupdate upstream/main-v2`
3. Resolve remaining conflicts: local-owned paths are auto-kept by the merge
   driver; review only the hook regions in `chat_tui.go` and any upstream
   changes to local-owned files (the sync script prints them).
4. `go build ./...` and `go test ./internal/cli/ ./internal/control/
   ./internal/agent/ -count=1`
5. Commit the merge.

`scripts/setup-merge-tools.ps1` installs the merge drivers and enables rerere.
`scripts/sync-upstream.ps1` runs steps 1–4 and reports the review list.

## Known deliberate deviations from upstream

- Tool cards, tables, banner, status bar and streaming rendering use the local
  opencode-style layer; upstream wrap cache/scroll state/mouse handling stays.
- Plan mode is non-blocking (no host approval gate); the plan list is archived
  at plan-turn end.
- Ask-tool mandate removed from prompts: proceed with a sensible default and
  state the assumption, or note the choice and stop for the user.
- Config paths/env vars keep upstream `reasonix` naming to minimize merges.
