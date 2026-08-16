; Setup for Windows.
;
; This exists because of what the plain binary had to do to itself. A program
; that copies its own executable into AppData and writes a Run key has, by any
; behavioural definition, dropped and persisted itself, and antivirus scores
; the sequence rather than the intent. Soos was killed as a trojan for doing
; exactly that.
;
; Moving those two steps in here changes the shape. Installing files and
; writing registry entries is what setup programs are for, the engine doing it
; is one every scanner has seen a million times, and the installed binary no
; longer touches either. What is left in Soos is reading a folder and uploading
; it, which is what he is for.
;
; Not a fix for being unsigned. Nothing here is.

#define AppName "Soos"
#define AppPublisher "Northwest Falls"
#define AppURL "https://northwestfalls.com/soos"

#ifndef AppVersion
  #define AppVersion "0.0.0"
#endif

[Setup]
AppId={{7C2E4A18-9F3B-4D6E-8A51-2B0C9E4F7D33}
AppName={#AppName}
AppVersion={#AppVersion}
AppVerName={#AppName} {#AppVersion}
AppPublisher={#AppPublisher}
AppPublisherURL={#AppURL}
AppSupportURL={#AppURL}
AppUpdatesURL={#AppURL}
VersionInfoVersion={#AppVersion}
VersionInfoCompany={#AppPublisher}
VersionInfoDescription={#AppName} Setup

; Per user, so there is no administrator prompt. The prompt is the step people
; cancel, and nothing here needs rights beyond the account running it.
PrivilegesRequired=lowest
DefaultDirName={localappdata}\Programs\Soos
DisableDirPage=yes
DisableProgramGroupPage=yes
UninstallDisplayName={#AppName}
UninstallDisplayIcon={app}\soos.exe

OutputDir=..\dist
OutputBaseFilename=SoosSetup
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible arm64
ArchitecturesInstallIn64BitMode=x64compatible arm64

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Files]
; Both builds ship in one download and only the right one lands, so nobody has
; to know what is inside their own laptop.
Source: "..\dist\soos-windows-arm64.exe"; DestDir: "{app}"; DestName: "soos.exe"; Check: IsArm64; Flags: ignoreversion
Source: "..\dist\soos-windows-amd64.exe"; DestDir: "{app}"; DestName: "soos.exe"; Check: not IsArm64; Flags: ignoreversion

[Registry]
; Persistence and shell integration, written by setup and removed with it.
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; \
  ValueType: string; ValueName: "Soos"; ValueData: """{app}\soos.exe"" tray"; \
  Flags: uninsdeletevalue

Root: HKCU; Subkey: "Software\Classes\*\shell\SoosShare"; \
  ValueType: string; ValueName: ""; ValueData: "Send to Northwest Falls"; \
  Flags: uninsdeletekey
Root: HKCU; Subkey: "Software\Classes\*\shell\SoosShare"; \
  ValueType: string; ValueName: "Icon"; ValueData: "{app}\soos.exe,0"
Root: HKCU; Subkey: "Software\Classes\*\shell\SoosShare\command"; \
  ValueType: string; ValueName: ""; ValueData: """{app}\soos.exe"" share ""%1"""

[Icons]
Name: "{userprograms}\Soos"; Filename: "{app}\soos.exe"

[Run]
; Straight into pairing, in a window that stays open, because the account and
; the folder are the two things setup cannot answer on its own.
Filename: "{app}\soos.exe"; Description: "Set up Soos now"; \
  Flags: postinstall nowait skipifsilent

[UninstallRun]
; The credential is no use to anyone once he is gone.
Filename: "{app}\soos.exe"; Parameters: "forget"; Flags: runhidden; RunOnceId: "SoosForget"
