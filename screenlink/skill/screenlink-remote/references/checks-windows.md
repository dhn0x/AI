# Windows server checks (PowerShell, read-only)

One command per `run`, short output. If the PowerShell window isn't running as Administrator, some commands return less. Say so instead of asking for elevation.

| # | Command | Look for |
|---|---|---|
| 1 | `hostname` | the right machine |
| 2 | `Get-CimInstance Win32_OperatingSystem \| Select-Object Caption,LastBootUpTime` | OS, recent unexpected reboot |
| 3 | `Get-PSDrive -PSProvider FileSystem \| Select-Object Name,Used,Free` | a drive almost full |
| 4 | `Get-CimInstance Win32_OperatingSystem \| Select-Object FreePhysicalMemory,TotalVisibleMemorySize` | low free memory |
| 5 | `Get-Process \| Sort-Object CPU -Descending \| Select-Object -First 10 Name,CPU,WS` | CPU or memory hogs |
| 6 | `Get-Service \| Where-Object {$_.StartType -eq 'Automatic' -and $_.Status -ne 'Running'} \| Select-Object -First 15 Name,Status` | automatic services that are stopped |
| 7 | `Get-Service <name> \| Select-Object Name,Status,StartType` | the product's service |
| 8 | `Get-EventLog -LogName System -EntryType Error -Newest 15 \| Select-Object TimeGenerated,Source,EventID` | repeating system errors |
| 9 | `Get-EventLog -LogName Application -EntryType Error -Newest 15 \| Select-Object TimeGenerated,Source,EventID` | application crashes |
| 10 | `Get-NetTCPConnection -State Listen \| Select-Object -First 25 LocalPort,OwningProcess` | expected ports listening |
| 11 | `w32tm /query /status` | time source and sync |
| 12 | `Test-NetConnection <host> -Port <port>` | reachability of a SIEM, collector or licence server (only hosts the engineer named) |
