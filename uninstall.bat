@echo off
rem ============================================================================
rem  modrinth-patcher uninstaller (Windows)
rem
rem  Reverses install.bat: closes Modrinth App, restores the original binary
rem  (--unpatch), removes the installed patcher + PATH entry + scheduled task.
rem
rem  Run locally:   uninstall.bat
rem  Run remotely:  PowerShell:  irm <url> -OutFile %TEMP%\mp.bat; cmd /c %TEMP%\mp.bat
rem
rem  Environment overrides:
rem    DEST_DIR=C:\Tools     where the patcher was installed
rem                          (default %LOCALAPPDATA%\Programs\modrinth-patcher)
rem    MP_BINARY=C:\path     the app binary to unpatch (default auto-detect)
rem    DRY_RUN=1             print actions without running them
rem ============================================================================
setlocal EnableDelayedExpansion

if defined DEST_DIR ( set "DEST_DIR=%DEST_DIR%" ) else ( set "DEST_DIR=%LOCALAPPDATA%\Programs\modrinth-patcher" )
if defined DRY_RUN ( set "DRY_RUN=%DRY_RUN%" ) else ( set "DRY_RUN=0" )
set "MP_EXE=%DEST_DIR%\modrinth-patcher.exe"

for /f %%A in ('echo prompt $E ^| cmd') do set "ESC=%%A"
set "C_GREEN=%ESC%[32m"
set "C_CYAN=%ESC%[36m"
set "C_YELLOW=%ESC%[33m"
set "C_RED=%ESC%[31m"
set "C_BOLD=%ESC%[1m"
set "C_DIM=%ESC%[2m"
set "C_RESET=%ESC%[0m"

echo %C_BOLD%%C_CYAN%==========================================================%C_RESET%
echo %C_BOLD%%C_CYAN%  modrinth-patcher - uninstaller%C_RESET%
echo %C_BOLD%%C_CYAN%  restores the original Modrinth App%C_RESET%
echo %C_BOLD%%C_CYAN%==========================================================%C_RESET%

rem ── 1. quit app ───────────────────────────────────────────────────────────
set "APP_BIN=%LOCALAPPDATA%\Modrinth App\Modrinth App.exe"
if not exist "%APP_BIN%" set "APP_BIN=%LOCALAPPDATA%\Programs\Modrinth App\Modrinth App.exe"
tasklist /FI "IMAGENAME eq Modrinth App.exe" 2>nul | find /i "Modrinth App.exe" >nul
if not errorlevel 1 (
    echo %C_CYAN%  Closing Modrinth App...%C_RESET%
    taskkill /IM "Modrinth App.exe" /F >nul 2>&1
    timeout /t 2 /nobreak >nul
    echo %C_GREEN%  Modrinth App closed%C_RESET%
) else (
    echo %C_GREEN%  Modrinth App not running%C_RESET%
)

rem ── 2. unpatch ────────────────────────────────────────────────────────────
if not exist "%MP_EXE%" (
    echo %C_YELLOW%  patcher not installed at %MP_EXE% - skipping unpatch%C_RESET%
) else (
    echo %C_CYAN%  Restoring original binary...%C_RESET%
    if defined MP_BINARY (
        "%MP_EXE%" --unpatch --binary "%MP_BINARY%"
    ) else (
        "%MP_EXE%" --unpatch
    )
    if errorlevel 1 (
        echo %C_RED%  unpatch reported a problem%C_RESET%
    ) else (
        echo %C_GREEN%  Original binary restored%C_RESET%
    )
)

if "%DRY_RUN%"=="1" (
    echo %C_DIM%  [dry-run] would: remove scheduled task, PATH entry, %MP_EXE%%C_RESET%
    exit /b 0
)

rem ── 3. remove scheduled task ──────────────────────────────────────────────
schtasks /Delete /TN "ModrinthPatcher" /F >nul 2>&1
if errorlevel 1 (
    echo %C_YELLOW%  no scheduled task found, or could not delete%C_RESET%
) else (
    echo %C_GREEN%  Removed scheduled task ModrinthPatcher%C_RESET%
)

rem ── 4. remove PATH entry ──────────────────────────────────────────────────
set "KEY=HKCU\Environment"
set "CURPATH="
for /f "tokens=1,2,* delims= " %%A in ('reg query "%KEY%" /v Path 2^>nul') do (
    if /i "%%A"=="Path" set "CURPATH=%%C"
)
set "PATHCHK=%TEMP%\mp-pathchk.txt"
set "DEST_DOT=!DEST_DIR:\=.!"
> "%PATHCHK%" echo !CURPATH!
findstr /i /c:"!DEST_DOT!" "%PATHCHK%" >nul
set "FOUND=!errorlevel!"
del /q "%PATHCHK%" >nul 2>&1
if "!FOUND!"=="0" (
    set "NEWPATH=!CURPATH:%DEST_DIR%=!"
    set "NEWPATH=!NEWPATH:;;=;!"
    if "!NEWPATH:~-1!"==";" set "NEWPATH=!NEWPATH:~0,-1!"
    reg add "%KEY%" /v Path /t REG_EXPAND_SZ /d "!NEWPATH!" /f >nul
    if errorlevel 1 (
        echo %C_YELLOW%  could not update PATH registry key%C_RESET%
    ) else (
        echo %C_GREEN%  Removed %DEST_DIR% from PATH%C_RESET%
    )
) else (
    echo %C_GREEN%  %DEST_DIR% not on PATH%C_RESET%
)

rem ── 5. remove files ───────────────────────────────────────────────────────
if exist "%MP_EXE%" (
    del /q "%MP_EXE%" >nul 2>&1
    if errorlevel 1 (
        echo %C_YELLOW%  could not remove %MP_EXE%%C_RESET%
    ) else (
        echo %C_GREEN%  Removed %MP_EXE%%C_RESET%
    )
)

echo.
echo %C_GREEN%  Uninstalled. The .orig backup next to the app binary is kept - delete it if unwanted.%C_RESET%
exit /b 0
