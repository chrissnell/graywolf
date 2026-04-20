; Graywolf NSIS installer
;
; Required defines (pass via /D on the makensis command line):
;   APP_VERSION         display version, e.g. 0.10.10
;   APP_VERSION_NUMERIC 4-part numeric version for VIProductVersion, e.g. 0.10.10.0
;   BINARY_PATH         absolute path to staged graywolf.exe
;   MODEM_PATH          absolute path to staged graywolf-modem.exe
;
; Optional:
;   OUTFILE             output installer filename

!ifndef APP_VERSION
  !error "APP_VERSION is required (pass /DAPP_VERSION=x.y.z)"
!endif
!ifndef APP_VERSION_NUMERIC
  !error "APP_VERSION_NUMERIC is required (pass /DAPP_VERSION_NUMERIC=x.y.z.0)"
!endif
!ifndef BINARY_PATH
  !error "BINARY_PATH is required"
!endif
!ifndef MODEM_PATH
  !error "MODEM_PATH is required"
!endif
!ifndef OUTFILE
  !define OUTFILE "graywolf_${APP_VERSION}_Windows_x86_64.exe"
!endif

!define APP_NAME      "Graywolf"
!define APP_PUBLISHER "chrissnell"
!define APP_EXE       "graywolf.exe"
!define MODEM_EXE     "graywolf-modem.exe"
!define APP_REGKEY    "Software\Microsoft\Windows\CurrentVersion\Uninstall\${APP_NAME}"

Name "${APP_NAME} ${APP_VERSION}"
OutFile "${OUTFILE}"
Unicode true
InstallDir "$PROGRAMFILES64\${APP_NAME}"
InstallDirRegKey HKLM "Software\${APP_NAME}" "InstallDir"
RequestExecutionLevel admin
SetCompressor /SOLID lzma

VIProductVersion "${APP_VERSION_NUMERIC}"
VIAddVersionKey  "ProductName"     "${APP_NAME}"
VIAddVersionKey  "CompanyName"     "${APP_PUBLISHER}"
VIAddVersionKey  "FileDescription" "${APP_NAME} APRS gateway"
VIAddVersionKey  "FileVersion"     "${APP_VERSION}"
VIAddVersionKey  "ProductVersion"  "${APP_VERSION}"
VIAddVersionKey  "LegalCopyright"  "${APP_PUBLISHER}"

!include "MUI2.nsh"
!include "x64.nsh"

!define MUI_ABORTWARNING

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "..\..\LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!define MUI_FINISHPAGE_RUN "$INSTDIR\${APP_EXE}"
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APP_NAME} now"
!define MUI_FINISHPAGE_RUN_FUNCTION LaunchAppShortcut
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES

!insertmacro MUI_LANGUAGE "English"

; ---------------------------------------------------------------------------
; Helpers
; ---------------------------------------------------------------------------

; Launches the Start Menu shortcut so the app starts with the proper args
; rather than re-implementing the same command line in two places.
Function LaunchAppShortcut
  ExecShell "" "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
FunctionEnd

; ---------------------------------------------------------------------------
; Init: 64-bit only, and silently uninstall any previous version in place
; ---------------------------------------------------------------------------

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "${APP_NAME} requires 64-bit Windows."
    Abort
  ${EndIf}
  SetRegView 64

  ReadRegStr $R0 HKLM "${APP_REGKEY}" "UninstallString"
  ReadRegStr $R1 HKLM "${APP_REGKEY}" "InstallLocation"
  StrCmp $R0 "" done

  MessageBox MB_OKCANCEL|MB_ICONQUESTION \
    "An existing ${APP_NAME} installation was found and will be removed before installing this version.$\n$\nYour data in C:\ProgramData\${APP_NAME} will be preserved.$\n$\nContinue?" \
    /SD IDOK IDOK uninst
  Abort

  uninst:
    ClearErrors
    ; _?=<dir> keeps the uninstaller running synchronously instead of
    ; respawning itself in TEMP, so we can wait for it to finish.
    ExecWait '"$R0" /S _?=$R1'
done:
FunctionEnd

Function un.onInit
  SetRegView 64
FunctionEnd

; ---------------------------------------------------------------------------
; Install
; ---------------------------------------------------------------------------

Section "Install"
  SetOutPath "$INSTDIR"
  File "/oname=${APP_EXE}"   "${BINARY_PATH}"
  File "/oname=${MODEM_EXE}" "${MODEM_PATH}"

  ; Writable data directory under C:\ProgramData. Same path the old MSI
  ; used, so existing users keep their database on upgrade.
  SetShellVarContext all
  CreateDirectory "$APPDATA\${APP_NAME}"

  ; Grant the local Users group Modify access so non-admin shortcuts can
  ; write the database. *S-1-5-32-545 is the well-known Users SID and is
  ; locale-independent, unlike the literal "Users" group name.
  nsExec::ExecToLog 'icacls "$APPDATA\${APP_NAME}" /grant "*S-1-5-32-545:(OI)(CI)M"'
  Pop $0

  ; Start Menu shortcut launches graywolf with the same arguments the
  ; previous Windows service used. The icon reuses the binary's own.
  CreateDirectory "$SMPROGRAMS\${APP_NAME}"
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk" \
    "$INSTDIR\${APP_EXE}" \
    '-config "$APPDATA\${APP_NAME}\graywolf.db" -history-db "$APPDATA\${APP_NAME}\graywolf-history.db" -modem "$INSTDIR\${MODEM_EXE}" -http 127.0.0.1:8080' \
    "$INSTDIR\${APP_EXE}" 0
  CreateShortCut "$SMPROGRAMS\${APP_NAME}\Uninstall ${APP_NAME}.lnk" \
    "$INSTDIR\uninstall.exe"

  ; Add/Remove Programs entry
  WriteRegStr   HKLM "Software\${APP_NAME}" "InstallDir" "$INSTDIR"
  WriteRegStr   HKLM "${APP_REGKEY}" "DisplayName"          "${APP_NAME}"
  WriteRegStr   HKLM "${APP_REGKEY}" "DisplayVersion"       "${APP_VERSION}"
  WriteRegStr   HKLM "${APP_REGKEY}" "Publisher"            "${APP_PUBLISHER}"
  WriteRegStr   HKLM "${APP_REGKEY}" "DisplayIcon"          "$INSTDIR\${APP_EXE}"
  WriteRegStr   HKLM "${APP_REGKEY}" "InstallLocation"      "$INSTDIR"
  WriteRegStr   HKLM "${APP_REGKEY}" "UninstallString"      '"$INSTDIR\uninstall.exe"'
  WriteRegStr   HKLM "${APP_REGKEY}" "QuietUninstallString" '"$INSTDIR\uninstall.exe" /S'
  WriteRegDWORD HKLM "${APP_REGKEY}" "NoModify" 1
  WriteRegDWORD HKLM "${APP_REGKEY}" "NoRepair" 1

  WriteUninstaller "$INSTDIR\uninstall.exe"
SectionEnd

; ---------------------------------------------------------------------------
; Uninstall
; ---------------------------------------------------------------------------

Section "Uninstall"
  SetShellVarContext all

  Delete "$INSTDIR\${APP_EXE}"
  Delete "$INSTDIR\${MODEM_EXE}"
  Delete "$INSTDIR\uninstall.exe"
  RMDir  "$INSTDIR"

  Delete "$SMPROGRAMS\${APP_NAME}\${APP_NAME}.lnk"
  Delete "$SMPROGRAMS\${APP_NAME}\Uninstall ${APP_NAME}.lnk"
  RMDir  "$SMPROGRAMS\${APP_NAME}"

  ; Intentionally do NOT delete $APPDATA\${APP_NAME} — preserve user data.

  DeleteRegKey HKLM "${APP_REGKEY}"
  DeleteRegKey HKLM "Software\${APP_NAME}"
SectionEnd
