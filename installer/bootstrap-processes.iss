// Ends every process whose executable lies in Folder: a Dwnl too old to be
// asked to quit, the Chrome bridge while the extension panel is open, or a
// download tool left behind. Only this user's processes report their path, and
// a process outside Folder (another copy of Dwnl included) is never touched.
// Returns how many processes were asked to end.
function EndProcessesIn(Folder: string): Integer;
var Locator, Service, Processes, Process: Variant;
  Prefix, Path: string;
  I: Integer;
begin
  Result := 0;
  Prefix := AddBackslash(Folder);
  try
    Locator := CreateOleObject('WbemScripting.SWbemLocator');
    Service := Locator.ConnectServer('.', 'root\CIMV2');
    Processes := Service.ExecQuery('SELECT Handle, ExecutablePath FROM Win32_Process WHERE ExecutablePath IS NOT NULL');
    for I := 0 to Processes.Count - 1 do begin
      Process := Processes.ItemIndex(I);
      Path := Process.ExecutablePath;
      if (Length(Path) > Length(Prefix)) and (CompareText(Copy(Path, 1, Length(Prefix)), Prefix) = 0) then begin
        Log('Ending ' + Path);
        Process.Terminate();
        Result := Result + 1;
      end;
    end;
  except
    Log('Could not end the processes in ' + Folder + ': ' + GetExceptionMessage);
  end;
end;
