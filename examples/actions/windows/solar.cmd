@echo off
rem Launcher for solar.ps1 -- set this .cmd as the Action's command_path.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dpn0.ps1" %*
