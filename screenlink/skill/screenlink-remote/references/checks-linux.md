# Linux server, SIEM or collector checks (read-only)

One command per `run`, short output. Replace `<service>` with the product's service name (for example a FortiSIEM collector, Wazuh, Elastic, Splunk forwarder). If you don't know the name, run `systemctl list-units --type=service --state=running --no-pager | head -n 40` first.

| # | Command | Look for |
|---|---|---|
| 1 | `hostname; uptime` | load average higher than the CPU count |
| 2 | `cat /etc/os-release \| head -n 3` | OS |
| 3 | `df -h \| grep -v tmpfs` | any filesystem ≥ 85% |
| 4 | `df -i \| grep -v tmpfs` | inodes ≥ 85% |
| 5 | `free -m` | low available memory, swap use |
| 6 | `top -bn1 \| head -n 15` | CPU or memory hogs |
| 7 | `systemctl --failed --no-pager \| head -n 20` | failed units |
| 8 | `systemctl status <service> --no-pager \| head -n 15` | the product's service is active and running |
| 9 | `journalctl -u <service> --since "2 hours ago" --no-pager \| tail -n 30` | errors from the product |
| 10 | `ss -tlnp \| head -n 30` | the expected ports are listening (for example 514/udp syslog, 443, 9200) |
| 11 | `timedatectl \| head -n 10` | clock synchronised (event timestamps, certificates) |
| 12 | `journalctl -p err --since "6 hours ago" --no-pager \| tail -n 25` | system errors |
| 13 | `dmesg -T \| grep -iE "oom\|killed process\|i/o error" \| tail -n 10` | out-of-memory kills, disk errors |

**For collectors and forwarders**, also check that events are arriving:
- `ss -uanp \| grep -E ":514\b" \| head` (syslog listener)
- `tail -n 5 <product log>`: recent timestamps mean events are flowing.
- Ask the engineer whether a `tcpdump` is allowed before running one. It can be heavy, and it shows client data.
