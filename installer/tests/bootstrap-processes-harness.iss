; This executable runs only InitializeSetup and then exits before any install step.
; It ends the processes running from {#FixtureRoot}\install-資料 and nothing else.
#ifndef FixtureRoot
  #error FixtureRoot is required
#endif
[Setup]
AppName=Dwnl process behavior tests
AppVersion=1
DefaultDirName={tmp}\Dwnl-unused-test-target
PrivilegesRequired=lowest
CreateAppDir=no
Uninstallable=no
UsePreviousAppDir=no
OutputDir={#FixtureRoot}
OutputBaseFilename=processes-harness
SetupLogging=no

[Code]
#include "..\bootstrap-processes.iss"

function InitializeSetup(): Boolean;
var Ended: Integer;
begin
  Ended := EndProcessesIn('{#FixtureRoot}\install-資料');
  if not SaveStringToFile('{#FixtureRoot}\results.txt', 'ended=' + IntToStr(Ended) + #10, False) then
    RaiseException('Could not save test results.');
  Result := False;
end;
