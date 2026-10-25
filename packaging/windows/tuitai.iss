; Bộ cài Windows cho Tui Tải (Inno Setup 6+)
; Build: iscc /DMyVersion=v0.0.1 /DMyExe=tuitai.exe /DMyIcon=tuitai.ico tuitai.iss
#ifndef MyVersion
  #define MyVersion "v0.0.1"
#endif
#ifndef MyExe
  #define MyExe "tuitai.exe"
#endif
#ifndef MyDir
  #define MyDir "."
#endif
#ifndef MyIcon
  #define MyIcon "tuitai.ico"
#endif

[Setup]
AppId={{8E2F7A31-5C4B-4D2E-9C77-2B1E9A0C4D51}
AppName=Tui Tải
AppVersion={#MyVersion}
AppVerName=Tui Tải {#MyVersion}
AppPublisher=captajn
AppPublisherURL=https://github.com/captajn/tuitai
AppSupportURL=https://github.com/captajn/tuitai/issues
DefaultDirName={autopf}\Tui Tai
DefaultGroupName=Tui Tải
DisableProgramGroupPage=yes
OutputBaseFilename=tuitai-setup-{#MyVersion}-x64
SetupIconFile={#MyIcon}
UninstallDisplayIcon={app}\tuitai.exe
Compression=lzma2/max
SolidCompression=yes
WizardStyle=modern
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
LicenseFile={#MyDir}\LICENSE
InfoBeforeFile={#MyDir}\README.md

[Languages]
Name: "vi"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Tạo lối tắt ngoài Desktop"; Flags: unchecked
Name: "extension"; Description: "Cài extension Chrome/Edge (gửi link sang app)"; Flags: checkedonce
Name: "autostart"; Description: "Chạy Tui Tải cùng Windows (nằm ở khay hệ thống)"; Flags: checkedonce

[Files]
Source: "{#MyDir}\{#MyExe}"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#MyIcon}"; DestDir: "{app}"; DestName: "tuitai.ico"; Flags: ignoreversion

[Icons]
Name: "{group}\Tui Tải"; Filename: "{app}\tuitai.exe"; Parameters: "gui"; WorkingDir: "{app}"
Name: "{group}\Gỡ cài đặt Tui Tải"; Filename: "{uninstallexe}"
Name: "{autodesktop}\Tui Tải"; Filename: "{app}\tuitai.exe"; Parameters: "gui"; Tasks: desktopicon

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; \
  ValueName: "TuiTai"; ValueData: """{app}\tuitai.exe"" gui --no-open --tray"; \
  Flags: uninsdeletevalue; Tasks: autostart

[Run]
Filename: "{app}\tuitai.exe"; Parameters: "extension export"; Flags: runhidden; Tasks: extension; StatusMsg: "Đang cài extension..."
Filename: "{app}\tuitai.exe"; Parameters: "gui"; Description: "Mở Tui Tải ngay"; Flags: postinstall nowait skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}"
