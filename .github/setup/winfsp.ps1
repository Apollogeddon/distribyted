# Installs WinFsp for the Windows runners, which mount through it in the FUSE tests. The release is
# pinned and its MSI checked against its SHA-256 (as winget's manifest for it lists), so a changed
# or replaced download fails here rather than installing.
$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

# The MSI with every feature (INSTALLLEVEL=1000), as winfsp/cgofuse's own CI does: winget's default
# level leaves out the headers a cgo build needs. Retried, as downloads on hosted runners can fail.
$msiUrl = 'https://github.com/winfsp/winfsp/releases/download/v2.1/winfsp-2.1.25156.msi'
$msiSha256 = '073A70E00F77423E34BED98B86E600DEF93393BA5822204FAC57A29324DB9F7A'

function Install-WinFsp {
  $maxAttempts = 5
  for ($attempt = 1; $attempt -le $maxAttempts; $attempt++) {
    try {
      $msiPath = Join-Path $env:RUNNER_TEMP 'winfsp.msi'
      Invoke-WebRequest -Uri $msiUrl -OutFile $msiPath
      $hash = (Get-FileHash -Algorithm SHA256 -Path $msiPath).Hash
      if ($hash -ne $msiSha256) { throw "the WinFsp MSI's SHA-256 is $hash, not $msiSha256" }

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
