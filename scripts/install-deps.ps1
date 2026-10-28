# Birrcare (رعاية بر) — install what a Windows PC needs (issue #68)
#
#   powershell -ExecutionPolicy Bypass -File .\scripts\install-deps.ps1 [-Yes] [-Lang ar|en]
#
# Recommended on Windows: run Birrcare inside WSL2 (Ubuntu), where the normal
# ./scripts/setup.sh works and Docker comes from Docker Desktop's WSL integration.
# This script:
#   - installs, with winget and after asking, what is missing: Git (with Git
#     Bash), jq, Docker Desktop, and optionally restic + rclone + sqlite3;
#   - tells you how to finish in WSL2 (recommended);
#   - or, if you choose to run natively, registers the update and backup timers
#     in Windows Task Scheduler (through Git Bash), like the Linux cron lines.
# It never installs anything without asking (unless -Yes).

param(
  [switch]$Yes,
  [ValidateSet("ar", "en")][string]$Lang = "en"
)

$ErrorActionPreference = "Stop"
$Root = (Resolve-Path (Join-Path $PSScriptRoot "..")).Path

function T([string]$En, [string]$Ar) { if ($Lang -eq "ar") { $Ar } else { $En } }

function Ask([string]$Question, [bool]$Default = $true) {
  if ($Yes) { return $Default }
  $hint = if ($Default) { "[Y/n]" } else { "[y/N]" }
  $answer = Read-Host "$Question $hint"
  if ([string]::IsNullOrWhiteSpace($answer)) { return $Default }
  return $answer.Trim().ToLower().StartsWith("y") -or $answer.Trim().StartsWith("ن")
}

function Have([string]$Command) { [bool](Get-Command $Command -ErrorAction SilentlyContinue) }

Write-Host (T "Birrcare: checking what this Windows PC needs" "رعاية بر: فحص ما يحتاجه جهاز Windows")

if (-not (Have "winget")) {
  Write-Host (T "winget (App Installer) is needed: install 'App Installer' from the Microsoft Store, then run this again." "يلزم winget (App Installer): ثبّته من Microsoft Store ثم أعد التشغيل.") -ForegroundColor Red
  exit 2
}

# Command to check → winget package id. Required first, then the optional backup tools.
$Required = [ordered]@{ "git" = "Git.Git"; "jq" = "jqlang.jq"; "docker" = "Docker.DockerDesktop" }
$Optional = [ordered]@{ "restic" = "restic.restic"; "rclone" = "Rclone.Rclone"; "sqlite3" = "SQLite.SQLite" }

$ToInstall = @()
foreach ($cmd in $Required.Keys) {
  if (Have $cmd) { Write-Host "  ✔ $cmd" } else { Write-Host ("  ✖ $cmd " + (T "(missing)" "(غير مثبت)")); $ToInstall += $Required[$cmd] }
}
$MissingOptional = @()
foreach ($cmd in $Optional.Keys) {
  if (Have $cmd) { Write-Host "  ✔ $cmd" } else { Write-Host ("  ✖ $cmd " + (T "(missing)" "(غير مثبت)")); $MissingOptional += $Optional[$cmd] }
}
if ($MissingOptional.Count -gt 0 -and (Ask (T "Install restic, rclone and sqlite3 for encrypted off-site backups?" "تثبيت restic و rclone و sqlite3 للنسخ الاحتياطي المشفر؟"))) {
  $ToInstall += $MissingOptional
}

if ($ToInstall.Count -gt 0) {
  Write-Host ((T "Will install with winget: " "سيتم التثبيت عبر winget: ") + ($ToInstall -join " "))
  if (-not (Ask (T "Go ahead?" "متابعة؟"))) { Write-Host (T "Nothing installed." "لم يُثبَّت شيء."); exit 0 }
  foreach ($id in $ToInstall) {
    winget install --id $id --exact --silent --accept-source-agreements --accept-package-agreements
    if ($LASTEXITCODE -ne 0) { Write-Host ((T "Could not install " "تعذر تثبيت ") + $id) -ForegroundColor Red }
  }
  if ($ToInstall -contains "Docker.DockerDesktop") {
    Write-Host (T "Open Docker Desktop once and finish its setup (it may ask to restart Windows)." "افتح Docker Desktop مرة واحدة وأكمل إعداده (قد يطلب إعادة تشغيل Windows).")
  }
} else {
  Write-Host (T "All the tools you chose are already installed." "كل الأدوات التي اخترتها مثبتة مسبقاً.") -ForegroundColor Green
}

Write-Host ""
Write-Host (T "Recommended: run Birrcare inside WSL2 (Ubuntu):" "المُوصى به: شغّل رعاية بر داخل WSL2 (Ubuntu):") -ForegroundColor Cyan
Write-Host "  1. wsl --install -d Ubuntu"
Write-Host (T "  2. Docker Desktop → Settings → Resources → WSL integration → turn on Ubuntu" "  2. Docker Desktop → Settings → Resources → WSL integration → فعّل Ubuntu")
Write-Host (T "  3. In Ubuntu: git clone <your repository> ~/birrcare && cd ~/birrcare && ./scripts/setup.sh" "  3. داخل Ubuntu: git clone <المستودع> ~/birrcare && cd ~/birrcare && ./scripts/setup.sh")
Write-Host ""

# Native Windows: the host scripts run through Git Bash, started by Task Scheduler.
if (Ask (T "Run Birrcare natively on Windows instead, and register the update and backup timers in Task Scheduler?" "تشغيل رعاية بر مباشرة على Windows بدلاً من ذلك، وتسجيل مؤقتات التحديث والنسخ الاحتياطي في Task Scheduler؟") $false) {
  $Bash = Join-Path $env:ProgramFiles "Git\bin\bash.exe"
  if (-not (Test-Path $Bash)) {
    Write-Host (T "Git Bash was not found (install Git first)." "لم يُعثر على Git Bash (ثبّت Git أولاً).") -ForegroundColor Red
    exit 1
  }
  # C:\Users\me\birrcare → /c/Users/me/birrcare for Git Bash.
  $Posix = "/" + $Root.Substring(0, 1).ToLower() + ($Root.Substring(2) -replace "\\", "/")
  # Name → command and minutes between runs (the same as the cron lines on Linux).
  $Tasks = [ordered]@{
    "Birrcare updater"           = @("./scripts/auto-deploy.sh", 5)
    "Birrcare backup"            = @("./scripts/backup.sh --if-due", 5)
    "Birrcare backup on request" = @("./scripts/backup.sh --if-requested", 1)
  }
  foreach ($name in $Tasks.Keys) {
    $command, $minutes = $Tasks[$name]
    $action = New-ScheduledTaskAction -Execute $Bash -Argument ("-lc `"cd '$Posix' && $command >> ~/birrcare-timers.log 2>&1`"")
    $trigger = New-ScheduledTaskTrigger -Once -At (Get-Date) -RepetitionInterval (New-TimeSpan -Minutes $minutes)
    $settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -StartWhenAvailable
    Register-ScheduledTask -TaskName $name -Action $action -Trigger $trigger -Settings $settings -Force | Out-Null
    Write-Host "  ✔ $name"
  }
  Write-Host (T "Timers registered (Task Scheduler, while you're signed in)." "سُجّلت المؤقتات (Task Scheduler، أثناء تسجيل دخولك).") -ForegroundColor Green
  Write-Host (T "Next, in Git Bash: ./scripts/deploy.sh" "بعد ذلك، في Git Bash: ./scripts/deploy.sh")
}
