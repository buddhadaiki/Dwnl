; This executable runs only InitializeSetup and then exits before any install step.
; It has no registry entries, payload, downloads, uninstaller, or Chrome integration.
#ifndef FixtureRoot
  #error FixtureRoot is required
#endif
[Setup]
AppName=Dwnl ownership behavior tests
AppVersion=1
DefaultDirName={tmp}\Dwnl-unused-test-target
PrivilegesRequired=lowest
CreateAppDir=no
Uninstallable=no
UsePreviousAppDir=no
OutputDir={#FixtureRoot}
OutputBaseFilename=ownership-harness
SetupLogging=no

[Code]
#include "..\bootstrap-ownership.iss"

var Report: string;

function Root(Name: string): string;
begin
  Result := '{#FixtureRoot}\' + Name;
end;

procedure RecordResult(Name: string; Value: Boolean);
begin
  Report := Report + Name + '=' + IntToStr(Ord(Value)) + #10;
end;

procedure RecordGate(Name: string; HasRegistration: Boolean; Registered: string);
begin
  RecordResult(Name, InstallationProblem(Root(Name), Root(Name), HasRegistration, Registered) = '');
end;

procedure ExerciseLifecycle();
var InstallRoot, Problem: string;
begin
  InstallRoot := Root('lifecycle-資料');
  Problem := PrepareOwnedDirectory(InstallRoot, InstallRoot, False, '');
  RecordResult('prepare-fresh', Problem = '');
  ForceDirectories(InstallRoot + '\state');
  SaveStringToFile(InstallRoot + '\native-host.json', UTF8Encode(ExpectedManifestFor(InstallRoot)), False);
  SaveStringToFile(InstallRoot + '\dwnl-host.exe', 'owned fixture', False);
  SaveStringToFile(InstallRoot + '\state\ledger.json', 'keep state bytes', False);
  SaveStringToFile(InstallRoot + '\unknown.txt', 'keep unknown bytes', False);
  SaveStringToFile(Root('download.mp3'), 'keep download bytes', False);
  DeleteIfOwned(InstallRoot + '\dwnl-host.exe', '1c9bfa0492e48f0fcd317bd4532710cf611d39ebab2f6213567c0f1ae0fb3a41');
  RecordResult('remove-owned-manifest', DeleteOwnedManifestFor(InstallRoot, InstallRoot + '\native-host.json'));
  RecordResult('reinstall-preserved-state', InstallationProblem(InstallRoot, InstallRoot, False, '') = '');
end;

function InitializeSetup(): Boolean;
var Files: TArrayOfString; Handle: THandle; Problem, InstallRoot: string;
begin
  Report := '';
  RecordGate('fresh', False, '');
  RecordGate('arbitrary', False, '');
  RecordGate('marker-valid-資料', False, '');
  RecordGate('marker-invalid', False, '');
  RecordGate('marker-moved', False, '');
  RecordGate('marker-reparse', False, '');
  RecordGate('root-reparse', False, '');
  RecordGate('manifest-altered', False, '');
  RecordGate('registration-external', True, Root('elsewhere\native-host.json'));
  RecordGate('registration-non-string', True, '');
  RecordGate('legacy', True, Root('legacy\native-host.json'));
  RecordGate('legacy-invalid-marker', True, Root('legacy-invalid-marker\native-host.json'));
  RecordResult('different-root', InstallationProblem(Root('different-root'), Root('expected-root'), False, '') = '');
  Files := ['dwnl-host.exe', 'native-host.json', '.dwnl-install-owner'];
  Handle := CreateFileW(Root('active\dwnl-host.exe'), GenericWrite, 0, 0, OpenExisting, FileAttributeNormal, 0);
  if Handle = InvalidHandleValue then RaiseException('The test could not lock its active fixture.');
  try
    RecordResult('active-file', FilesInactiveFor(Root('active'), Files));
  finally
    CloseHandle(Handle);
  end;
  RecordResult('inactive-file', FilesInactiveFor(Root('active'), Files));
  Handle := CreateFileW(Root('marker-valid-資料\.dwnl-install-owner'), GenericWrite, 0, 0, OpenExisting, FileAttributeNormal, 0);
  if Handle = InvalidHandleValue then RaiseException('The test could not lock its ownership marker.');
  try
    RecordResult('active-marker', FilesInactiveFor(Root('marker-valid-資料'), Files));
  finally
    CloseHandle(Handle);
  end;
  InstallRoot := Root('legacy');
  Problem := PrepareOwnedDirectory(InstallRoot, InstallRoot, True, InstallRoot + '\native-host.json');
  RecordResult('legacy-migrated', (Problem = '') and FileExists(InstallRoot + '\.dwnl-install-owner'));
  ExerciseLifecycle();
  InstallRoot := Root('failed-first-copy');
  Problem := PrepareOwnedDirectory(InstallRoot, InstallRoot, False, '');
  ForceDirectories(InstallRoot);
  SaveStringToFile(InstallRoot + '\dwnl-host.exe', 'partial failed copy', False);
  RecordResult('retry-failed-first-copy', (Problem = '') and
    (InstallationProblem(InstallRoot, InstallRoot, False, '') = ''));
  RecordResult('preserve-replaced-manifest', not DeleteOwnedManifestFor(Root('manifest-altered'), Root('manifest-altered\native-host.json')));
  InstallRoot := Root('failed-registration');
  Problem := PrepareOwnedDirectory(InstallRoot, InstallRoot, False, '');
  SaveStringToFile(InstallRoot + '\native-host.json', UTF8Encode(ExpectedManifestFor(InstallRoot)), False);
  RecordResult('compensate-new-manifest', (Problem = '') and CompensateNewManifestFor(InstallRoot, False));
  RecordResult('retry-failed-registration', InstallationProblem(InstallRoot, InstallRoot, False, '') = '');
  RecordResult('preserve-working-manifest', not CompensateNewManifestFor(Root('legacy'), True));
  RecordResult('preserve-compensation-replacement', not CompensateNewManifestFor(Root('manifest-altered'), False));
  if not SaveStringToFile('{#FixtureRoot}\results.txt', UTF8Encode(Report), False) then
    RaiseException('Could not save test results.');
  Result := False;
end;
