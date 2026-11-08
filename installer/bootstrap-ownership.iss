// Filesystem ownership rules shared by the real setup and its isolated Pascal harness.
const
  GenericWrite = $40000000;
  CreateNew = 1;
  OpenExisting = 3;
  FileAttributeNormal = $80;
  InvalidHandleValue = -1;
  FileAttributeReparsePoint = $400;
  InvalidFileAttributes = $FFFFFFFF;
  OwnerFileName = '.dwnl-install-owner';

function CreateFileW(FileName: string; Access, Share: Cardinal; Security: Integer;
  Creation, Flags: Cardinal; Template: Integer): THandle;
  external 'CreateFileW@kernel32.dll stdcall';
function CloseHandle(Handle: THandle): Boolean;
  external 'CloseHandle@kernel32.dll stdcall';
function GetFileAttributesW(FileName: string): Cardinal;
  external 'GetFileAttributesW@kernel32.dll stdcall';
function WriteFile(Handle: THandle; Buffer: AnsiString; Count: Cardinal;
  var Written: Cardinal; Overlapped: Integer): Boolean;
  external 'WriteFile@kernel32.dll stdcall';
function FlushFileBuffers(Handle: THandle): Boolean;
  external 'FlushFileBuffers@kernel32.dll stdcall';
function CreateDirectoryW(Path: string; Security: Integer): Boolean;
  external 'CreateDirectoryW@kernel32.dll stdcall';

function IsReparsePath(Path: string): Boolean;
var Attributes: Cardinal; Cursor: string;
begin
  Result := False; Cursor := Path;
  while Length(Cursor) > 3 do begin
    Attributes := GetFileAttributesW(Cursor);
    if (Attributes <> InvalidFileAttributes) and ((Attributes and FileAttributeReparsePoint) <> 0) then begin
      Result := True; Exit;
    end;
    Cursor := ExtractFileDir(Cursor);
  end;
end;

function JsonEscape(Value: string): string;
begin
  Result := Value;
  StringChangeEx(Result, '\', '\\', True);
  StringChangeEx(Result, '"', '\"', True);
end;

function ExpectedManifestFor(InstallRoot: string): string;
begin
  Result := '{' + #10 + '  "name": "com.dwnl.host",' + #10 +
    '  "description": "Dwnl private native helper",' + #10 +
    '  "path": "' + JsonEscape(AddBackslash(InstallRoot) + 'dwnl-host.exe') + '",' + #10 +
    '  "type": "stdio",' + #10 +
    '  "allowed_origins": ["chrome-extension://{#ExtensionId}/"]' + #10 + '}' + #10;
end;

function ManifestMatchesFor(InstallRoot: string): Boolean;
var ManifestPath: string; Bytes: AnsiString;
begin
  Result := False; ManifestPath := AddBackslash(InstallRoot) + 'native-host.json';
  if IsReparsePath(ManifestPath) then Exit;
  if not LoadStringFromFile(ManifestPath, Bytes) then Exit;
  Result := Bytes = UTF8Encode(ExpectedManifestFor(InstallRoot));
end;

function RegistrationMatchesFor(InstallRoot, Registered: string): Boolean;
begin
  Result := (CompareText(Registered, AddBackslash(InstallRoot) + 'native-host.json') = 0) and
    ManifestMatchesFor(InstallRoot);
end;

function ExpectedOwnerFor(InstallRoot: string): string;
begin
  // This is a durable product/path claim, not a secret or a permission to erase data.
  Result := 'Dwnl private installation owner v1' + #10 +
    'AppId={B01F7A32-74CA-4C9E-BDD2-635B305A5480}' + #10 +
    'HostName=com.dwnl.host' + #10 +
    'AllowedOrigin=chrome-extension://{#ExtensionId}/' + #10 +
    'InstallRoot=' + InstallRoot + #10;
end;

function OwnerMatchesFor(InstallRoot: string): Boolean;
var Path: string; Bytes: AnsiString;
begin
  Result := False; Path := AddBackslash(InstallRoot) + OwnerFileName;
  if IsReparsePath(Path) or not LoadStringFromFile(Path, Bytes) then Exit;
  Result := Bytes = UTF8Encode(ExpectedOwnerFor(InstallRoot));
end;

function InstallationProblem(InstallRoot, ExpectedRoot: string; HasRegistration: Boolean;
  Registered: string): string;
begin
  Result := '';
  if CompareText(InstallRoot, ExpectedRoot) <> 0 then begin
    Result := 'O Dwnl só é instalado na pasta dele, dentro do seu usuário do Windows.'; Exit;
  end;
  if IsReparsePath(InstallRoot) then begin
    Result := 'O Dwnl não aceita atalhos de pasta (pontos de nova análise) no caminho de instalação.'; Exit;
  end;
  if FileExists(InstallRoot) then begin
    Result := 'O caminho de instalação do Dwnl não é uma pasta.'; Exit;
  end;
  if (GetFileAttributesW(AddBackslash(InstallRoot) + OwnerFileName) <> InvalidFileAttributes) and
     not OwnerMatchesFor(InstallRoot) then begin
    Result := 'A marca de instalação do Dwnl não corresponde a esta pasta. Os arquivos foram preservados.'; Exit;
  end;
  if (GetFileAttributesW(AddBackslash(InstallRoot) + 'native-host.json') <> InvalidFileAttributes) and
     not ManifestMatchesFor(InstallRoot) then begin
    Result := 'Já existe uma configuração de conexão com o Chrome que não pertence a este instalador.'; Exit;
  end;
  if HasRegistration and not RegistrationMatchesFor(InstallRoot, Registered) then begin
    Result := 'Já existe um registro de conexão com o Chrome que não pertence a este instalador.'; Exit;
  end;
  if DirExists(InstallRoot) and not OwnerMatchesFor(InstallRoot) and
     not (HasRegistration and RegistrationMatchesFor(InstallRoot, Registered)) then
    Result := 'Não foi possível confirmar que a pasta do Dwnl pertence a esta instalação. Os arquivos foram preservados.';
end;

function PrepareOwnedDirectory(InstallRoot, ExpectedRoot: string; HasRegistration: Boolean;
  Registered: string): string;
var Handle: THandle; Bytes: AnsiString; Written: Cardinal; Saved, HadDirectory: Boolean;
begin
  HadDirectory := DirExists(InstallRoot);
  Result := InstallationProblem(InstallRoot, ExpectedRoot, HasRegistration, Registered);
  if Result <> '' then Exit;
  if OwnerMatchesFor(InstallRoot) then Exit;
  // If another directory appeared since validation, CREATE_DIRECTORY fails rather
  // than letting the new directory inherit our ownership claim.
  if not HadDirectory and not CreateDirectoryW(InstallRoot, 0) then begin
    Result := 'Não foi possível criar a pasta de instalação do Dwnl.'; Exit;
  end;
  if IsReparsePath(InstallRoot) then begin
    Result := 'O Dwnl não aceita atalhos de pasta (pontos de nova análise) no caminho de instalação.'; Exit;
  end;
  // CREATE_NEW never overwrites another marker. Persist before the first payload copy
  // so a failed install can be retried without deleting state or trusting its name.
  Handle := CreateFileW(AddBackslash(InstallRoot) + OwnerFileName, GenericWrite,
    0, 0, CreateNew, FileAttributeNormal, 0);
  if Handle = InvalidHandleValue then begin
    Result := 'Não foi possível criar a marca de instalação do Dwnl.'; Exit;
  end;
  Bytes := UTF8Encode(ExpectedOwnerFor(InstallRoot));
  Written := 0;
  try
    Saved := WriteFile(Handle, Bytes, Length(Bytes), Written, 0) and
      (Written = Length(Bytes)) and FlushFileBuffers(Handle);
  finally
    CloseHandle(Handle);
  end;
  if not Saved or not OwnerMatchesFor(InstallRoot) then
    Result := 'Não foi possível conferir a marca de instalação do Dwnl. Nada foi registrado.';
end;

function DeleteOwnedManifestFor(InstallRoot, Registered: string): Boolean;
begin
  Result := RegistrationMatchesFor(InstallRoot, Registered) and
    DeleteFile(AddBackslash(InstallRoot) + 'native-host.json');
end;

function CompensateNewManifestFor(InstallRoot: string; WasOwnedInstallation: Boolean): Boolean;
begin
  // Preserve both a working installation and a manifest replaced after our write.
  Result := not WasOwnedInstallation and ManifestMatchesFor(InstallRoot) and
    DeleteFile(AddBackslash(InstallRoot) + 'native-host.json');
end;

function FilesInactiveFor(InstallRoot: string; Files: TArrayOfString): Boolean;
var I: Integer; Path: string; Handle: THandle;
begin
  Result := False;
  for I := 0 to GetArrayLength(Files) - 1 do begin
    Path := AddBackslash(InstallRoot) + Files[I];
    if IsReparsePath(Path) then Exit;
    if FileExists(Path) then begin
      Handle := CreateFileW(Path, GenericWrite, 0, 0, OpenExisting, FileAttributeNormal, 0);
      if Handle = InvalidHandleValue then Exit;
      CloseHandle(Handle);
    end;
  end;
  Result := True;
end;

procedure DeleteIfOwned(Path, Expected: string);
begin
  if not IsReparsePath(Path) and FileExists(Path) and (CompareText(GetSHA256OfFile(Path), Expected) = 0) then
    DeleteFile(Path);
end;
