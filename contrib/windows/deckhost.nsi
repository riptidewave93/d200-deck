; Windows installer for deckhost: Program Files, for every user (asks for admin).
; Built from the repo root: makensis -NOCD -DVERSION=... -DOUTFILE=... contrib/windows/deckhost.nsi
Unicode true
!define APP "D200 Deck"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\D200Deck"
!define RUN_KEY "Software\Microsoft\Windows\CurrentVersion\Run"

Name "${APP}"
OutFile "${OUTFILE}"
RequestExecutionLevel admin
InstallDir "$PROGRAMFILES64\${APP}"
SetCompressor /SOLID lzma

!include "MUI2.nsh"
!define MUI_FINISHPAGE_RUN
!define MUI_FINISHPAGE_RUN_FUNCTION StartTray
!define MUI_FINISHPAGE_RUN_TEXT "Start ${APP}"
!insertmacro MUI_PAGE_LICENSE "LICENSE"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

; the Start menu, Run key and uninstall entry are for every user
Function .onInit
	SetShellVarContext all
	SetRegView 64
FunctionEnd

Function un.onInit
	SetShellVarContext all
	SetRegView 64
FunctionEnd

; start the tray as the user, not with the installer's admin rights: Explorer
; opens the shortcut
Function StartTray
	Exec '"$WINDIR\explorer.exe" "$SMPROGRAMS\${APP}.lnk"'
FunctionEnd

; stop the daemon and the tray so their files can be replaced
!macro StopDeckhost
	nsExec::Exec '"$INSTDIR\deckhost.exe" exit'
	nsExec::Exec 'taskkill /F /IM deckhost.exe'
	Sleep 500
!macroend

Section
	!insertmacro StopDeckhost
	SetOutPath "$INSTDIR"
	File "build\deckhost.exe"
	File "LICENSE"
	File "NOTICE.md"
	File /r "LICENSES"
	WriteUninstaller "$INSTDIR\uninstall.exe"

	CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\deckhost.exe" "tray"
	WriteRegStr HKLM "${RUN_KEY}" "D200Deck" '"$INSTDIR\deckhost.exe" tray'

	WriteRegStr HKLM "${UNINST_KEY}" "DisplayName" "${APP}"
	WriteRegStr HKLM "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
	WriteRegStr HKLM "${UNINST_KEY}" "Publisher" "d200-deck project"
	WriteRegStr HKLM "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\deckhost.exe"
	WriteRegStr HKLM "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
	WriteRegStr HKLM "${UNINST_KEY}" "UninstallString" '"$INSTDIR\uninstall.exe"'
	WriteRegDWORD HKLM "${UNINST_KEY}" "NoModify" 1
	WriteRegDWORD HKLM "${UNINST_KEY}" "NoRepair" 1
SectionEnd

Section "Uninstall"
	!insertmacro StopDeckhost
	Delete "$SMPROGRAMS\${APP}.lnk"
	DeleteRegValue HKLM "${RUN_KEY}" "D200Deck"
	DeleteRegKey HKLM "${UNINST_KEY}"
	RMDir /r "$INSTDIR"
SectionEnd
