# Installs the World Recorder addon into the WoW Forever client and the uploader as a login item, then links this computer to the player's wowdb.wiki account (Windows).
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'
$base = 'https://wowdb.wiki/assets/world-recorder/1.2.1'
$work = Join-Path ([IO.Path]::GetTempPath()) ([IO.Path]::GetRandomFileName())
New-Item -ItemType Directory $work | Out-Null
try {
  $download = Join-Path $work 'download'
  $exe = Join-Path $work 'WorldRecorderSetup.exe'
  Invoke-WebRequest "$base/WorldRecorderSetup.exe" -OutFile $download -UseBasicParsing
  Move-Item $download $exe
  if ($env:WOW_DIR) { & $exe install "--wow=$($env:WOW_DIR)" } else { & $exe install }
  if ($LASTEXITCODE -ne 0) { throw 'World Recorder setup did not finish.' }
} finally {
  Remove-Item $work -Recurse -Force -ErrorAction SilentlyContinue
}
