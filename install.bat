@echo off
rem ============================================================================
rem  modrinth-patcher installer (Windows)
rem
rem  Installs the newest modrinth-patcher build, quits the Modrinth App if it
rem  is running, patches ads out, and relaunches the app.
rem
rem  Run locally:   install.bat
rem  Run remotely:  PowerShell:  iex (irm <url>)        (see README one-liner)
rem
rem  Environment overrides:
rem    DEST_DIR=C:\Tools     install destination  (default %LOCALAPPDATA%\Programs\modrinth-patcher)
rem    MP_REPO=owner/repo    GitHub repo          (default NZ-Linix/modrinth-patcher)
rem    MP_REF=main           branch/tag/commit    (default main)
rem    GH_TOKEN=...          token for downloads (only needed for private forks)
rem    MP_BINARY=C:\path     patch a specific app binary instead of auto-detect
rem    DRY_RUN=1             print actions without quitting/patching/relaunching
rem ============================================================================
setlocal EnableDelayedExpansion

if defined DEST_DIR ( set "DEST_DIR=%DEST_DIR%" ) else ( set "DEST_DIR=%LOCALAPPDATA%\Programs\modrinth-patcher" )
if defined MP_REPO ( set "REPO=%MP_REPO%" ) else ( set "REPO=NZ-Linix/modrinth-patcher" )
if defined MP_REF ( set "REF=%MP_REF%" ) else ( set "REF=main" )
if defined DRY_RUN ( set "DRY_RUN=%DRY_RUN%" ) else ( set "DRY_RUN=0" )
set "BIN_NAME=modrinth-patcher-windows-amd64.exe"

rem ── tiny TUI ───────────────────────────────────────────────────────────────
rem enable ANSI on modern Windows 10+ (harmless no-op elsewhere)
for /f %%A in ('echo prompt $E ^| cmd') do set "ESC=%%A"
set "C_GREEN=%ESC%[32m"
set "C_CYAN=%ESC%[36m"
set "C_YELLOW=%ESC%[33m"
set "C_RED=%ESC%[31m"
set "C_BOLD=%ESC%[1m"
set "C_DIM=%ESC%[2m"
set "C_RESET=%ESC%[0m"

echo %C_BOLD%%C_CYAN%==========================================================%C_RESET%
echo %C_BOLD%%C_CYAN%  modrinth-patcher - remove ads from Modrinth App%C_RESET%
echo %C_BOLD%%C_CYAN%  https://github.com/%REPO%%C_RESET%
echo %C_BOLD%%C_CYAN%==========================================================%C_RESET%

rem ── helpers ────────────────────────────────────────────────────────────────
set "MP_EXE=%DEST_DIR%\modrinth-patcher.exe"
set "APP_BIN=%LOCALAPPDATA%\Modrinth App\Modrinth App.exe"
if not exist "%APP_BIN%" set "APP_BIN=%LOCALAPPDATA%\Programs\Modrinth App\Modrinth App.exe"
if not exist "%APP_BIN%" set "APP_BIN="

call :find_patcher_proc
set "APP_RUNNING=!FOUND!"

rem ── 1. pick binary (local dist or download) ───────────────────────────────
set "SRC="
if exist "%~dp0dist\%BIN_NAME%" (
    set "SRC=%~dp0dist\%BIN_NAME%"
    echo %C_GREEN%  Using local build: !SRC!%C_RESET%
) else (
    echo %C_CYAN%  Downloading %BIN_NAME%...%C_RESET%
    call :download_binary
    if errorlevel 1 (
        echo %C_RED%  Download failed - for a private fork set GH_TOKEN and retry%C_RESET%
        exit /b 1
    )
    echo %C_GREEN%  Downloaded to !SRC!%C_RESET%
)

rem ── 2. install ────────────────────────────────────────────────────────────
if not exist "%DEST_DIR%" mkdir "%DEST_DIR%"
copy /Y "!SRC!" "%MP_EXE%" >nul
if errorlevel 1 (
    echo %C_RED%  could not copy to %DEST_DIR%%C_RESET%
    exit /b 1
)
echo %C_GREEN%  Installed %MP_EXE%%C_RESET%

if "%DRY_RUN%"=="1" (
    echo %C_DIM%  [dry-run] would: close Modrinth App, run patcher, relaunch app%C_RESET%
    exit /b 0
)

rem ── 3. quit app ───────────────────────────────────────────────────────────
if not defined APP_BIN (
    echo %C_GREEN%  Modrinth App not found - skipping close/relaunch%C_RESET%
    goto :patch
)
if "!APP_RUNNING!"=="1" (
    echo %C_CYAN%  Closing Modrinth App...%C_RESET%
    taskkill /IM "Modrinth App.exe" /F >nul 2>&1
    timeout /t 2 /nobreak >nul
    echo %C_GREEN%  Modrinth App closed%C_RESET%
) else (
    echo %C_GREEN%  Modrinth App not running%C_RESET%
)

:patch
rem ── 4. patch ──────────────────────────────────────────────────────────────
echo %C_CYAN%  Patching ads out...%C_RESET%
if defined MP_BINARY (
    "%MP_EXE%" --binary "%MP_BINARY%"
) else (
    "%MP_EXE%"
)
if errorlevel 1 (
    echo %C_RED%  Patch failed%C_RESET%
    exit /b 1
)
echo %C_GREEN%  Ads patched%C_RESET%

rem ── 5. relaunch ───────────────────────────────────────────────────────────
if defined APP_BIN (
    start "" "%APP_BIN%"
    echo %C_GREEN%  Modrinth App relaunched%C_RESET%
)

echo.
echo %C_GREEN%  Done - ads removed. The watcher re-patches after updates automatically.%C_RESET%
exit /b 0

rem ── subroutines ───────────────────────────────────────────────────────────
:find_patcher_proc
set "FOUND=0"
tasklist /FI "IMAGENAME eq Modrinth App.exe" 2>nul | find /i "Modrinth App.exe" >nul && set "FOUND=1"
exit /b 0

:download_binary
set "TMPDL=%TEMP%\mp-patcher-%BIN_NAME%"
if exist "%TMPDL%" del /q "%TMPDL%" >nul 2>&1
if defined GH_TOKEN (
    call curl -fsSL -L -H "Authorization: Bearer %GH_TOKEN%" -H "Accept: application/vnd.github.raw" "https://api.github.com/repos/%REPO%/contents/dist/%BIN_NAME%?ref=%REF%" -o "%TMPDL%"
) else (
    call curl -fsSL "https://raw.githubusercontent.com/%REPO%/%REF%/dist/%BIN_NAME%" -o "%TMPDL%" 2>nul
    if errorlevel 1 (
        call curl -fsSL -L -H "Accept: application/vnd.github.raw" "https://api.github.com/repos/%REPO%/contents/dist/%BIN_NAME%?ref=%REF%" -o "%TMPDL%"
    )
)
if errorlevel 1 exit /b 1
set "SRC=%TMPDL%"
exit /b 0
