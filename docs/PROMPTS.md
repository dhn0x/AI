# Prompts for ScreenLink

Paste these into Claude Code (or Codex) **after** the client has shared their screen and given you control in Teams.

Before you start:
- Ask the client not to use their mouse or keyboard while you have control.
- **Ctrl+Alt+Q** stops the AI at any time.

---

## 1. Quick connection test

```
Use screenlink to work on the client's screen in my Teams meeting.

1. list_windows and attach to the Teams meeting window (ms-teams.exe) that shows the shared screen.
2. read the screen and tell me what you see.
3. click into the terminal window on the shared screen (click on the prompt text).
4. run: whoami
5. run: hostname

Keep outputs short. For each run, tell me the output and how long it took.
If you get SAFETY STOP, stop and tell me; don't keep retrying.
Never type passwords. If you see a password prompt, ask me.
```

## 2. FortiSOAR full health and licence check (read-only)

The client has an SSH session open to the FortiSOAR server (user `csadmin`).

```
Use screenlink. I'm in a Teams meeting with a client and have remote control of their screen.
They have an SSH session open to their FortiSOAR server (user csadmin).
Task: full health check of FortiSOAR, including the licence. Find any problem and its root cause.

SETUP
1. list_windows, attach to the Teams meeting window (ms-teams.exe), read the screen, tell me what you see.
2. Click into the SSH terminal on the shared screen. Confirm the prompt shows the FortiSOAR host.

RULES
- READ-ONLY. Do NOT restart/stop services, edit files, run upgrades, change or upload a licence,
  or run anything that changes the server. If a fix is needed, explain it and ask me first.
- Never type passwords. If sudo or anything asks for a password, stop and tell me.
- Never print secrets: no licence key text, no passwords, no private keys, no tokens.
- Keep output short: | tail -n 30, | head -n 30, grep. OCR reads only what is visible.
- One command per run. If a command is not found, note it and move on.
- If you get SAFETY STOP or "stopped": stop and ask me. Remind me to ask the client not to use
  their mouse/keyboard while we work.
- OCR can misread 0/O and 1/l. Double-check numbers and dates before concluding.

CHECKS (summarise each result in one line as you go)

A. System
1. hostname; uptime
2. cat /etc/redhat-release
3. df -h | grep -v tmpfs
4. df -i | grep -v tmpfs                       (inodes full also breaks FortiSOAR)
5. free -m
6. top -bn1 | head -n 15
7. timedatectl | head -n 10                    (wrong time breaks licence, certificates and HA)

B. FortiSOAR version and licence (READ-ONLY)
8. rpm -qa | grep -i cyops | sort | head -n 20   (installed version)
9. csadm license --show-details                 (edition, expiry, users/actions; DO NOT change it)
10. csadm license --get-device-uuid              (only to compare with the licence)
   Report: licence type, expiry date, days left, limits and how much is used, and any warning.
   Flag it if it expires within 30 days or the device UUID does not match.

C. Services
11. csadm services --status
12. systemctl --failed --no-pager | head -n 20
13. systemctl status postgresql* --no-pager | head -n 12
14. systemctl status elasticsearch --no-pager | head -n 12
15. systemctl status rabbitmq-server --no-pager | head -n 12
16. systemctl status nginx --no-pager | head -n 8

D. Workflows and queues
17. ls -lt /var/log/cyops/ | head -n 15
18. grep -iE "error|exception|fail" /var/log/cyops/cyops-workflow/celeryd.log | tail -n 25
19. grep -iE "error|exception" /var/log/cyops/cyops-api/*.log | tail -n 20
20. grep -iE "error|exception|fail" /var/log/cyops/cyops-integrations/*.log | tail -n 20  (connectors)

E. High availability (only if this is an HA cluster)
21. csadm ha show-health
22. csadm ha list-nodes

F. System errors
23. journalctl -p err --since "6 hours ago" --no-pager | tail -n 25
24. dmesg -T | grep -iE "oom|killed process|i/o error" | tail -n 10

REPORT
At the end, give me:
- Licence: type, expiry, days left, usage against limits, OK or problem.
- Health table: area / status / evidence (which command showed it).
- Root cause of the issue (most likely) and the fix, step by step, with risk level.
Wait for my OK before doing anything that changes the server.
```

> `csadm` options differ between FortiSOAR versions. If one says "unknown option", ask the AI to run `csadm <area> --help` and pick the right one.

## 3. Generic Linux SIEM or collector check (read-only)

```
Use screenlink. Attach to the Teams meeting window, click into the SSH terminal on the shared screen.
READ-ONLY checks, one command per run, short output, never type passwords, stop on SAFETY STOP.

1. hostname; uptime
2. df -h | grep -v tmpfs
3. free -m
4. top -bn1 | head -n 15
5. systemctl --failed --no-pager | head -n 20
6. systemctl status <service-name> --no-pager | head -n 15
7. journalctl -u <service-name> --since "2 hours ago" --no-pager | tail -n 30
8. ss -tlnp | head -n 30                       (listening ports)
9. timedatectl | head -n 10

Then tell me what is wrong, the evidence, and the fix (ask before changing anything).
```

## 4. Windows server check (PowerShell, read-only)

```
Use screenlink. Attach to the Teams meeting window, click into the PowerShell window on the shared screen.
READ-ONLY checks, one command per run, short output, never type passwords, stop on SAFETY STOP.

1. hostname
2. Get-CimInstance Win32_OperatingSystem | Select-Object Caption,LastBootUpTime
3. Get-PSDrive -PSProvider FileSystem | Select-Object Name,Used,Free
4. Get-Service | Where-Object {$_.StartType -eq 'Automatic' -and $_.Status -ne 'Running'} | Select-Object -First 15 Name,Status
5. Get-EventLog -LogName System -EntryType Error -Newest 15 | Select-Object TimeGenerated,Source,EventID
6. Get-Process | Sort-Object CPU -Descending | Select-Object -First 10 Name,CPU,WS

Then tell me what is wrong, the evidence, and the fix (ask before changing anything).
```
