@echo off
rem install.bat — installs the newest modrinth-patcher build from .\dist
rem onto the system PATH as modrinth-patcher.
rem
rem Default destination: %LOCALAPPDATA%\Programs\modrinth-patcher
rem Override with: set DEST_DIR=C:\Tools && install.bat
setlocal EnableDelayedExpansion

set "SCRIPT_DIR=%~dp0"
set "DIST_DIR=%SCRIPT_DIR%dist"
if defined DEST_DIR (
	set "DEST_DIR=%DEST_DIR%"
) else (
	set "DEST_DIR=%LOCALAPPDATA%\Programs\modrinth-patcher"
)

rem --- 1. pick the binary ------------------------------------------------
set "SRC="
if exist "%DIST_DIR%\modrinth-patcher-windows-amd64.exe" (
	set "SRC=%DIST_DIR%\modrinth-patcher-windows-amd64.exe"
)
if "%SRC%"=="" (
	echo error: no Windows binary found in %DIST_DIR% ^(build one first: go build^)
	exit /b 1
)

rem --- 2. install (always overwrites) ------------------------------------
if not exist "%DEST_DIR%" mkdir "%DEST_DIR%"
copy /Y "%SRC%" "%DEST_DIR%\modrinth-patcher.exe" >nul
if errorlevel 1 (
	echo error: could not copy to %DEST_DIR%
	exit /b 1
)

rem --- 3. add to user PATH if not already present ------------------------
rem Read the current user PATH. The value line looks like:
rem     Path    REG_EXPAND_SZ    C:\foo;C:\bar
rem so we take the 3rd token onward (skip name + type).
set "KEY=HKCU\Environment"
set "CURPATH="
for /f "tokens=1,2,* delims= " %%A in ('reg query "%KEY%" /v Path 2^>nul') do (
	if /i "%%A"=="Path" set "CURPATH=%%C"
)

rem Check via a temp file (the pipe + delayed expansion combo is
rem unreliable). Use "." in place of "\" in the search so it matches on
rem both real Windows and wine's findstr.
set "PATHCHK=%TEMP%\mp-pathchk.txt"
set "DEST_DOT=!DEST_DIR:\=.!"
> "%PATHCHK%" echo !CURPATH!
findstr /i /c:"!DEST_DOT!" "%PATHCHK%" >nul
set "FOUND=!errorlevel!"
del /q "%PATHCHK%" >nul 2>&1
if "!FOUND!"=="1" (
	rem append to the user PATH (no machine-wide changes, no admin needed)
	if "!CURPATH!"=="" (
		set "NEWPATH=!DEST_DIR!"
	) else (
		set "NEWPATH=!CURPATH!;!DEST_DIR!"
	)
	reg add "%KEY%" /v Path /t REG_EXPAND_SZ /d "!NEWPATH!" /f >nul
	if errorlevel 1 (
		echo warning: could not update PATH registry key
	) else (
		echo added to user PATH: !DEST_DIR!
	)
) else (
	echo already on PATH: !DEST_DIR!
)

rem --- 4. verify ---------------------------------------------------------
"%DEST_DIR%\modrinth-patcher.exe" --version >nul 2>&1
if errorlevel 1 (
	echo warning: installed but --version failed
) else (
	echo installed: %SRC% -^> %DEST_DIR%\modrinth-patcher.exe
	"%DEST_DIR%\modrinth-patcher.exe" --version
)

rem note: PATH changes apply to NEW terminals only
echo note: open a new terminal for PATH changes to take effect
endlocal
