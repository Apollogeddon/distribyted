# Installs WinFsp for the Windows runners, which mount through it in the FUSE tests. GITHUB_TOKEN
# lifts the GitHub API's rate limit.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# The MSI with every feature (INSTALLLEVEL=1000), as winfsp/cgofuse's own CI does: winget's default
# level leaves out the headers a cgo build needs. Retried, as downloads on hosted runners can fail.
function Install-WinFsp {
  $maxAttempts = 5
  for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
    try {
      $headers = @{ Authorization = "token $env:GITHUB_TOKEN" }
      $release = Invoke-RestMethod -Uri 'https://api.github.com/repos/winfsp/winfsp/releases/latest' -Headers $headers
      $msiUrl = ($release.assets | Where-Object { $_.name -match '^winfsp-.*\.msi$' } | Select-Object -First 1).browser_download_url
      if (-not $msiUrl) { throw "no winfsp-*.msi asset found on release $($release.tag_name)" }

      $msiPath = Join-Path $env:RUNNER_TEMP 'winfsp.msi'
      Invoke-WebRequest -Uri $msiUrl -OutFile $msiPath

      $proc = Start-Process -FilePath msiexec.exe -ArgumentList "/i `"$msiPath`" /qn INSTALLLEVEL=1000" -Wait -PassThru
      if ($proc.ExitCode -ne 0) { throw "msiexec exited $($proc.ExitCode)" }

      # fails here, rather than as a confusing cgo error in a later build
      $header = 'C:\Program Files (x86)\WinFsp\inc\fuse\fuse_common.h'
      if (-not (Test-Path $header)) { throw "WinFsp installed but $header is still missing" }
      return
    } catch {
      if ($attempt -eq $maxAttempts) {
        Write-Error "installing WinFsp failed after $maxAttempts attempts: $_"
        exit 1
      }
      $delay = 10 * $attempt
      Write-Warning "installing WinFsp failed (attempt $attempt/$maxAttempts): $_; retrying in ${delay}s..."
      Start-Sleep -Seconds $delay
    }
  }
}

Install-WinFsp

# the headers, for a build with cgo
echo "CPATH=C:\Program Files\WinFsp\inc\fuse;C:\Program Files (x86)\WinFsp\inc\fuse" | Out-File -FilePath $env:GITHUB_ENV -Encoding utf8 -Append
