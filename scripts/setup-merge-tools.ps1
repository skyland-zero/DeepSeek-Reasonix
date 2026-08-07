$ErrorActionPreference = 'Stop'

# Enable rerere so repeated conflict resolutions are reused automatically.
git config rerere.enabled true

# Merge drivers used by the .gitattributes keep-ours policy.
git config merge.keep-ours.driver true

Write-Host 'Merge tooling installed: rerere enabled, keep-ours driver registered.'
Write-Host 'Run scripts/sync-upstream.ps1 for the next upstream sync.'
