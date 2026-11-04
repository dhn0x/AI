---
name: screenlink-remote
description: Troubleshoot a client's system through a remote-control or screen-sharing window on this PC (Microsoft Teams, Zoom, Webex, AnyDesk, TeamViewer, RDP, Citrix, VM consoles) using the ScreenLink MCP tools. Use when the engineer says they have remote control / a shared screen of a client machine and wants checks run, a SIEM/SOAR/server investigated, or a health report made. Read-only by default.
---

# ScreenLink remote troubleshooting

You work on a **client's computer that is only reachable as a picture in a window on this PC**: a meeting with remote control, a remote-desktop tool, or a VM console. The ScreenLink MCP server (`screenlink` tools) captures that window, reads it with local OCR and types as real key presses.

If the `screenlink` tools aren't available, tell the engineer to connect them (`claude mcp add screenlink -- <path>\screenlink.exe`, or `[mcp_servers.screenlink]` in Codex), and stop.

**When a direct path exists, prefer it.** If the server is reachable over SSH or through a another authorized remote-management agent, use that instead: it gives exact output and costs fewer tokens. ScreenLink is for when the screen is the only way in.

## 1. Start safely

1. Remind the engineer to tell the client:
   - an AI assistant is helping;
   - **the client must not use their mouse or keyboard while we have control**, including on a second display.
2. `list_windows`, then find the remote tool's window using **[references/remote-tools.md](references/remote-tools.md)** (process and title for each tool).
3. `attach` to it, then `read` and describe what you see: which tool, which machine, and whether you have control.
4. If you see a terminal, **click into it once** (`click` on the prompt text, or x,y from `read` with `boxes=true`). Typing is refused until you have clicked: that's what stops keys landing on the client's other display.

## 2. Rules (always)

- **Read-only unless the engineer says otherwise.** Don't restart or stop services, edit config, delete, upgrade, or change licences or users. Propose fixes and wait for an explicit OK.
- **Never type passwords**, and never read out secrets (licence keys, tokens, private keys). At a password or sudo prompt, ask the engineer.
- **One command per `run`, and short output:** `| tail -n 30`, `| head -n 30`, `grep`, `Select-Object -First 20`. OCR only reads what's visible.
- **SAFETY STOP** means the typed command didn't appear correctly on the shared screen, so Enter was not pressed. Don't blindly retry. Tell the engineer that the client may be using their PC or another display; retry only once after they confirm.
- **"stopped"** means the engineer pressed Ctrl+Alt+Q. Stop and ask.
- **OCR mistakes** (`0`/`O`, `1`/`l`/`|`, `5`/`S`): double-check numbers, dates, IPs and names before drawing conclusions. Re-`read` a `region`, or `look` if needed.
- Prefer `read` (text) over `look` (image). Use `look` only for graphics.
- Don't do anything outside the task: no browsing, no email, no other windows.

## 3. Run the checks

Pick the check list that fits the target:

| Target | Check list |
|---|---|
| FortiSOAR | [references/checks-fortisoar.md](references/checks-fortisoar.md) |
| Linux server, SIEM, collector | [references/checks-linux.md](references/checks-linux.md) |
| Windows server | [references/checks-windows.md](references/checks-windows.md) |

Summarise each result in one line as you go: *what you checked, OK or problem, and the key value*. If the engineer gives a symptom ("playbooks stuck", "collector down"), start with the checks for that area, then do the rest.

## 4. Report

At the end, give the engineer:

1. **Root cause** (most likely), with the evidence: which command showed it.
2. A **health table**: area / status / evidence.
3. **The fix, step by step**, with a risk level, and wait for their OK.

If they want a file, write the results as JSON (format in [references/report-format.md](references/report-format.md)) and run:

```
python scripts/health_report.py findings.json report.xlsx
```

This makes an Excel workbook with a summary sheet and a colour-coded checks sheet.
