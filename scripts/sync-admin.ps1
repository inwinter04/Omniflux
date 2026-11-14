$ErrorActionPreference = 'Stop'
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root 'web\admin\dist'
$targetRoot = Join-Path $root 'internal\console'
$target = Join-Path $targetRoot 'dist'

if (-not (Test-Path (Join-Path $dist 'index.html'))) {
  throw 'web/admin/dist/index.html not found; run the admin build first'
}

if (Test-Path $target) {
  $stale = Get-ChildItem -LiteralPath $target -Force | Where-Object { $_.Name -ne '.gitkeep' }
  $stale | Remove-Item -Recurse -Force
} else {
  New-Item -ItemType Directory -Path $target | Out-Null
}

Get-ChildItem -LiteralPath $dist -Force | Copy-Item -Destination $target -Recurse -Force
if (-not (Test-Path (Join-Path $target '.gitkeep'))) {
  New-Item -ItemType File -Path (Join-Path $target '.gitkeep') | Out-Null
}
Write-Host "synced admin assets to $target"