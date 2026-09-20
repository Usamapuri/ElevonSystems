@echo off
setlocal EnableExtensions
REM ===========================================================================
REM  Elevon POS - silent printing launcher (Windows)
REM ===========================================================================
REM  The POS prints by writing a finished slip into an off-screen iframe and
REM  calling window.print(). In a normal browser that raises the print dialog,
REM  which is unusable on a till: the cashier would confirm a dialog after
REM  every sale. Chrome and Edge started with --kiosk-printing skip the dialog
REM  and send every job straight to the machine's DEFAULT printer.
REM
REM  So: set the thermal printer as the Windows default, then start the POS
REM  with this script instead of a normal browser window.
REM
REM  Usage:
REM      kiosk-print.cmd                       -> http://localhost:3000
REM      kiosk-print.cmd https://pos.example   -> that URL
REM
REM  Notes:
REM   - --kiosk-printing only takes effect on a browser process started with
REM     the flag. A separate --user-data-dir is used below so the flag is not
REM     swallowed by an already-running ordinary browser.
REM   - The browser prints to the DEFAULT printer. Per-document routing (a
REM     receipt to the thermal printer, an A4 invoice to the office laser)
REM     needs the desktop app of Phase 8, which prints through window.elevon.
REM   - Put a shortcut to this file in the Startup folder (shell:startup) to
REM     have the till come up printing silently after a reboot.
REM ===========================================================================

set "POS_URL=%~1"
if "%POS_URL%"=="" set "POS_URL=http://localhost:3000"

set "PROFILE_DIR=%LOCALAPPDATA%\ElevonPOS\kiosk-profile"
if not exist "%PROFILE_DIR%" mkdir "%PROFILE_DIR%" >nul 2>&1

set "BROWSER="
for %%P in (
  "%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe"
  "%ProgramFiles%\Microsoft\Edge\Application\msedge.exe"
  "%ProgramFiles%\Google\Chrome\Application\chrome.exe"
  "%ProgramFiles(x86)%\Google\Chrome\Application\chrome.exe"
  "%LOCALAPPDATA%\Google\Chrome\Application\chrome.exe"
) do if not defined BROWSER if exist %%P set "BROWSER=%%~P"

if not defined BROWSER (
  echo.
  echo   Could not find Microsoft Edge or Google Chrome in the usual places.
  echo   Install either one, or edit this file and set BROWSER to its path.
  echo.
  pause
  exit /b 1
)

echo Starting Elevon POS with silent printing:
echo   browser : %BROWSER%
echo   url     : %POS_URL%
echo   profile : %PROFILE_DIR%
echo.
echo Receipts will go to the Windows DEFAULT printer with no dialog.

start "" "%BROWSER%" ^
  --kiosk-printing ^
  --user-data-dir="%PROFILE_DIR%" ^
  --no-first-run ^
  --no-default-browser-check ^
  --disable-features=Translate,TranslateUI ^
  --start-maximized ^
  --app="%POS_URL%"

endlocal
exit /b 0
