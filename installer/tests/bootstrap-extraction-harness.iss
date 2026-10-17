; Extract only an already acquired and pinned official archive inside the fixture.
; InitializeSetup returns False before any installation, registry or uninstaller step.
#ifndef FixtureRoot
  #error FixtureRoot is required
#endif
[Setup]
AppName=Dwnl supplier extraction behavior tests
AppVersion=1
DefaultDirName={tmp}\Dwnl-unused-extraction-test-target
PrivilegesRequired=lowest
CreateAppDir=no
Uninstallable=no
UsePreviousAppDir=no
OutputDir={#FixtureRoot}
OutputBaseFilename=extraction-harness
SetupLogging=no
#include "..\bootstrap-archive-settings.iss"

[Code]
function InitializeSetup(): Boolean;
var Report, Digest: string;
begin
  try
    if CompareText(GetSHA256OfFile('{#ArchivePath}'), '{#ArchiveHash}') <> 0 then
      RaiseException('The official fixture archive does not match its pin.');
    ExtractArchive('{#ArchivePath}', '{#FixtureRoot}\extracted', '', True, nil);
    Digest := GetSHA256OfFile('{#FixtureRoot}\extracted\{#SelectedEntry}');
    Report := 'ok' + #10 + Lowercase(Digest) + #10;
  except
    Report := 'error' + #10 + GetExceptionMessage() + #10;
  end;
  if not SaveStringToFile('{#FixtureRoot}\extraction-result.txt', UTF8Encode(Report), False) then
    RaiseException('Could not save extraction test results.');
  Result := False;
end;
