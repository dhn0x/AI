# Findings JSON for scripts/health_report.py

Write the results to a JSON file like this, then run `python scripts/health_report.py findings.json report.xlsx`. It needs `openpyxl` (`pip install openpyxl`).

```json
{
  "title": "FortiSOAR health check",
  "client": "Client name",
  "host": "fortisoar01",
  "engineer": "Engineer name",
  "date": "2026-11-04",
  "method": "Teams remote control via ScreenLink",
  "licence": {
    "type": "Enterprise",
    "expiry": "2027-03-31",
    "days_left": 147,
    "usage": "18 / 25 users",
    "status": "OK"
  },
  "checks": [
    {"area": "System", "check": "Disk (df -h)", "status": "PROBLEM", "evidence": "/var 96% used", "note": "Elasticsearch data growing"},
    {"area": "Services", "check": "csadm services --status", "status": "WARN", "evidence": "celeryd restarting", "note": ""},
    {"area": "Licence", "check": "csadm license --show-details", "status": "OK", "evidence": "expires 2027-03-31", "note": ""}
  ],
  "root_cause": "The /var filesystem is 96% full, so Elasticsearch went read-only and playbooks stopped.",
  "fix": [
    "Confirm with the client before any change.",
    "Clean old logs under /var/log/cyops (list them first, don't delete blindly).",
    "Extend /var or move the Elasticsearch data; re-check df -h."
  ]
}
```

- `status` is one of `OK`, `WARN`, `PROBLEM` or `UNKNOWN`. It sets the colour in the workbook.
- Never put secrets in it: no licence key text, passwords or tokens.
- `licence`, `root_cause` and `fix` are optional. Leave `licence` out for non-FortiSOAR checks.
