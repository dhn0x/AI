# FortiSOAR checks (read-only)

The client is in an SSH session on the FortiSOAR server, usually as `csadmin`. Run one command per `run`. If a command or option is "not found" or "unknown", note it and move on. `csadm` options differ between versions; `csadm <area> --help` shows the right one.

**Don't change anything:** no service restarts, no `csadm` commands that set or upload anything, and no licence changes. Never print the licence key text.

## A. System
| # | Command | Look for |
|---|---|---|
| 1 | `hostname; uptime` | load average higher than the CPU count |
| 2 | `cat /etc/redhat-release` | OS version |
| 3 | `df -h \| grep -v tmpfs` | any filesystem ≥ 85% (`/`, `/var`, `/var/lib/pgsql`, `/var/lib/elasticsearch`). A full disk is the most common FortiSOAR problem. |
| 4 | `df -i \| grep -v tmpfs` | inodes ≥ 85% |
| 5 | `free -m` | available memory under 10%, swap heavily used |
| 6 | `top -bn1 \| head -n 15` | one process at 100% CPU, or using all memory |
| 7 | `timedatectl \| head -n 10` | clock not synchronised or wrong timezone (breaks the licence, certificates and HA) |

## B. Version and licence
| # | Command | Look for |
|---|---|---|
| 8 | `rpm -qa \| grep -i cyops \| sort \| head -n 20` | installed version; mixed versions after a failed upgrade |
| 9 | `csadm license --show-details` | edition, **expiry date**, **days left**, user and action limits compared with usage |
| 10 | `csadm license --get-device-uuid` | compare with the UUID the licence was issued for |

Report: licence type, expiry, days left, usage against limits. **Flag** an expiry within 30 days, limits reached or almost reached, or a UUID mismatch (needs a rehost through a Fortinet support ticket).

## C. Services
| # | Command | Look for |
|---|---|---|
| 11 | `csadm services --status` | any service not running |
| 12 | `systemctl --failed --no-pager \| head -n 20` | failed units |
| 13 | `systemctl status postgresql* --no-pager \| head -n 12` | database down, recovery mode |
| 14 | `systemctl status elasticsearch --no-pager \| head -n 12` | search down (the UI shows empty lists or errors) |
| 15 | `systemctl status rabbitmq-server --no-pager \| head -n 12` | queue broker down (playbooks don't start) |
| 16 | `systemctl status nginx --no-pager \| head -n 8` | web UI down |

## D. Workflows, API, connectors
| # | Command | Look for |
|---|---|---|
| 17 | `ls -lt /var/log/cyops/ \| head -n 15` | which logs are changing now |
| 18 | `grep -iE "error\|exception\|fail" /var/log/cyops/cyops-workflow/celeryd.log \| tail -n 25` | playbook failures, worker crashes |
| 19 | `grep -iE "error\|exception" /var/log/cyops/cyops-api/*.log \| tail -n 20` | API errors, database errors |
| 20 | `grep -iE "error\|exception\|fail" /var/log/cyops/cyops-integrations/*.log \| tail -n 20` | connector failures (credentials, network, certificates) |

## E. High availability (only if clustered)
| # | Command | Look for |
|---|---|---|
| 21 | `csadm ha show-health` | node or replication not healthy |
| 22 | `csadm ha list-nodes` | missing node, wrong role |

## F. System errors
| # | Command | Look for |
|---|---|---|
| 23 | `journalctl -p err --since "6 hours ago" --no-pager \| tail -n 25` | repeating errors |
| 24 | `dmesg -T \| grep -iE "oom\|killed process\|i/o error" \| tail -n 10` | out-of-memory kills, disk errors |

## Common root causes (match the symptom to the evidence)
| Symptom | Usual cause | Evidence |
|---|---|---|
| UI slow or errors, lists empty | Elasticsearch down, or disk full | 3, 14 |
| Playbooks stuck "Active" or not starting | RabbitMQ or celery workers down; disk full | 11, 15, 18 |
| Connector actions fail | Credentials expired, network or proxy, certificate | 20 |
| Licence warning or features disabled | Expired or near expiry, limits reached, UUID changed, wrong clock | 7, 9, 10 |
| Everything slow | Memory pressure, OOM kills, high load | 5, 6, 24 |
