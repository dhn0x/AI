# ScreenLink

**Let Claude Code or Codex troubleshoot a client's system through a Microsoft Teams remote-control session, with nothing installed on the client.**

ScreenLink is a small MCP server that runs on the **engineer's Windows PC**. When a client shares their screen in Teams and gives you control, ScreenLink lets the AI:

- **read** the client's screen as text, using the Windows built-in OCR on your PC;
- **type** commands as real key presses through the Teams window;
- **run** a command and get back only its output, in about 3–8 seconds.

It's much faster than screenshot-based "computer use": the AI gets **text**, not images, and everything heavy (capture, OCR, change detection) happens locally.

```
 Client PC                     Engineer PC
┌──────────────┐  Teams    ┌──────────────────────────────────────────┐
│ SSH / Power- │ ◀──────── │ Teams meeting window (shared screen)     │
│ Shell / SIEM │  control  │        ▲ keys          │ capture         │
└──────────────┘           │  ┌─────┴──────────────▼──────────────┐   │
                           │  │ screenlink.exe (MCP, local OCR)   │   │
                           │  └─────────────▲─────────────────────┘   │
                           │                │ MCP (stdio)             │
                           │        Claude Code / Codex               │
                           └──────────────────────────────────────────┘
```

---

## Requirements

- Windows 10 or 11 on the engineer's PC, with the English OCR language (installed by default on English Windows).
  To check, run this in PowerShell:
  ```powershell
  [Windows.Media.Ocr.OcrEngine,Windows.Foundation,ContentType=WindowsRuntime] | Out-Null
  [Windows.Media.Ocr.OcrEngine]::AvailableRecognizerLanguages | % LanguageTag
  ```
- Go 1.22 or newer, to build from source.
- Claude Code or Codex.

## Build

```powershell
git clone https://github.com/<your-org>/screenlink.git
cd screenlink
go test .
go build -o screenlink.exe .
.\screenlink.exe version
```

## Connect it to the AI

**Claude Code:**
```powershell
claude mcp add screenlink -- C:\path\to\screenlink.exe
```
Restart Claude Code, then run `/mcp`; `screenlink` should show as connected.

So the tools aren't blocked in auto mode, add an allow rule with `/permissions`: `mcp__screenlink`.

**Codex** (`%USERPROFILE%\.codex\config.toml`):
```toml
[mcp_servers.screenlink]
command = 'C:\path\to\screenlink.exe'
```

## Using it in a Teams meeting

1. **Tell the client:**
   - an AI assistant is helping you;
   - **while you have control, they must not use their mouse or keyboard**, including on a second display. Teams sends your keys to whichever window they're using.
2. The client **shares their screen** (the display with the terminal on it) and **gives you control**.
3. On your PC, **maximize the Teams meeting window** and keep it on your main screen, not minimized or covered.
4. The client (or you) opens a terminal: an SSH session or PowerShell, with a big font (about 14–16 pt).
5. Ask the AI, for example:
   > Use screenlink: attach to the Teams meeting window, read the screen, click into the terminal, then run `hostname`.

Ready-made prompts, including a full FortiSOAR health and licence check, are in **[docs/PROMPTS.md](docs/PROMPTS.md)**.

To see which windows ScreenLink can see, run `screenlink.exe windows`.

## Tools

| Tool | What it does |
|---|---|
| `list_windows` | Lists windows to attach to (Teams meeting, pop-out, RDP…). |
| `attach` | Locks onto the window that shows the client's screen. |
| `status` | Shows the attached window, whether it's stopped, and the log path. |
| `read` | OCR of the window as text. `boxes` adds each line's position; `changes_only` returns only new lines. |
| `look` | A small image, only when graphics matter (charts, icons). |
| `click` | Clicks by visible text or x,y. Remembers the spot as the "typing target". |
| `run` | Types a command, checks it, presses Enter, waits and returns the output. |
| `type` / `keys` | Typing text, or keys and shortcuts (`ctrl+c`, `tab`, `enter`…). |
| `scroll` | Mouse wheel. |
| `wait` | Waits until the screen settles (long jobs), then reads it. |

## Safety

ScreenLink is built for working on **client production systems**.

- **Kill switch:** **Ctrl+Alt+Q** stops it at once. Press it again to allow it again. The AI can't press it.
- **Only the attached window:** it acts only while that window is in front. If focus is lost, typing stops.
- **Second-display protection:**
  - It clicks into the client's terminal on the shared screen before typing, before erasing, and before pressing Enter.
  - It types the command **without Enter**, checks that the whole command is visible on the shared screen, and only then presses Enter.
  - If the text is missing or wrong, it stops with **SAFETY STOP** and nothing runs.
  - Short commands and risky commands must match exactly.
- **Risky commands** (delete, stop or restart a service, reboot, kill, firewall, users, packages and so on) need a **Yes** from the engineer in a pop-up on their PC.
- **Passwords:** it refuses to type at a password prompt. The engineer or client types passwords.
- **Banner:** a banner at the bottom of the engineer's screen shows that the AI is working and how to stop it.
- **Audit log:** every action is logged with the engineer's Windows name to `%LOCALAPPDATA%\ScreenLink\logs\<date>.jsonl`. Text typed with `type` is logged only as a length.
  > The log contains what was on the client's screen. Treat it as client data.

## Settings (environment variables)

| Variable | Default | Meaning |
|---|---|---|
| `SCREENLINK_KEY_GAP_MS` | `35` | Pause between typed keys. Raise it if keys get lost on a slow Teams link. |
| `SCREENLINK_OCR_SCALE` | `2` | Upscaling before OCR. Try `3` for tiny fonts. |

## Limits

- OCR can confuse `0`/`O` and `1`/`l`/`|`. The AI is told to double-check values.
- It reads only what's visible, so keep output short (`| tail -n 30`, `grep`).
- Terminal prompts with icons or powerline glyphs read badly. Plain server prompts read well.
- Teams doesn't pass the clipboard through. If the shared picture freezes, ScreenLink waits.
- **It can't stop the client from using their PC.** The checks prevent wrong commands from running, but stray letters can land in the client's window if they keep working.
- Windows only (engineer side). The client side can be any OS that Teams can share.

## Project layout

| File | Contents |
|---|---|
| `main.go` | MCP server (JSON-RPC over stdio), tool list and AI instructions. |
| `link.go` | Tools, safety checks, command verification, log. |
| `ocr.go` | Windows OCR worker (PowerShell + `Windows.Media.Ocr`), row rebuilding. |
| `win.go` | Win32: window capture, keyboard and mouse input, kill switch, banner. |
| `match_test.go` | Tests for the "is the typed command really on screen" check. |
