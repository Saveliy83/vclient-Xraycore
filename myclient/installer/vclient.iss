#ifndef AppVersion
#define AppVersion "0.1.0"
#endif

[Setup]
AppId={{0E97CC8B-B9E4-497C-8C97-6F4DC8A8A50E}
AppName=vclient
AppVersion={#AppVersion}
AppPublisher=vclient
DefaultDirName={autopf}\vclient
DefaultGroupName=vclient
UninstallDisplayIcon={app}\vclient-gui.exe
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=admin
CloseApplications=force
RestartApplications=no
OutputDir=..\dist
OutputBaseFilename=vclient-setup-{#AppVersion}
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
Uninstallable=yes

[Languages]
Name: "russian"; MessagesFile: "compiler:Languages\Russian.isl"

[Tasks]
Name: "desktopicon"; Description: "Создать ярлык на рабочем столе"; GroupDescription: "Дополнительные значки:"; Flags: unchecked

[Files]
Source: "payload\vclient-gui.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "payload\wintun.dll"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\vclient"; Filename: "{app}\vclient-gui.exe"; WorkingDir: "{app}"
Name: "{autodesktop}\vclient"; Filename: "{app}\vclient-gui.exe"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
Filename: "{app}\vclient-gui.exe"; Description: "Запустить vclient"; Flags: postinstall skipifsilent nowait

[Code]
var
  RemoveUserData: Boolean;

function InitializeSetup(): Boolean;
var
  StatePath: String;
begin
  Result := True;
  StatePath := ExpandConstant('{userappdata}\vclient\tun-state.json');
  if FileExists(StatePath) then
  begin
    MsgBox(
      'Сначала выключите VPN и дождитесь восстановления сети или запустите vclient от администратора для восстановления.',
      mbError, MB_OK);
    Result := False;
  end;
end;

function InitializeUninstall(): Boolean;
begin
  Result := True;
  RemoveUserData := MsgBox(
    'Удалить настройки и профили?',
    mbConfirmation, MB_YESNO or MB_DEFBUTTON2) = IDYES;
end;

procedure CurUninstallStepChanged(CurUninstallStep: TUninstallStep);
var
  UserDataPath: String;
begin
  if (CurUninstallStep = usPostUninstall) and RemoveUserData then
  begin
    UserDataPath := ExpandConstant('{userappdata}\vclient');
    if DirExists(UserDataPath) and not DelTree(UserDataPath, True, True, True) then
      MsgBox('Не удалось полностью удалить пользовательские данные: ' + UserDataPath,
        mbError, MB_OK);
  end;
end;
