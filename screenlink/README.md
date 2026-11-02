# ScreenLink

**Let Claude Code or Codex troubleshoot a client's system through a remote-control window, with no ScreenLink installation on the client.**

ScreenLink is a small MCP server that runs on the **engineer's Windows PC**. When a client shares their screen through a remote tool and gives you keyboard and mouse control, ScreenLink lets the AI:

- **read** the client's screen as text, using the Windows built-in OCR on your PC;
- **type** commands as real key presses through the attached remote-session window;
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
git clone https://github.com/dhn0x/AI.git
cd AI/screenlink
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

## Remote tools and screen sharing

ScreenLink attaches to a visible window on your Windows PC; it does not require a Teams-specific integration. Remote tools such as **Microsoft Teams, AnyDesk, TeamViewer, RustDesk, RDP, and browser-based remote sessions** are candidates, provided their viewer forwards keyboard and mouse input to the remote computer.

**Screen sharing alone allows viewing, not control.** The remote tool must grant keyboard and mouse control before Claude can click, type, or run commands. The client still needs the remote tool or session required to share their desktop, but does not need ScreenLink installed.

Claude Code (or Codex) and ScreenLink run on the engineer's Windows PC and communicate through the local MCP connection configured above. After attaching to the remote-session window, the AI can read visible text with OCR, click, type text, and enter commands in a visible terminal. This setup does not give a cloud chat direct access to your desktop.

Compatibility depends on the viewer's screen capture, focus, input forwarding, permissions, and privilege level. **The listed tools are not a verified compatibility list:** test each viewer on a harmless task before using it for client work. ScreenLink's stop switch and command approval checks still apply.

### General connection steps

1. Open the remote session and have the client grant keyboard and mouse control.
2. Keep the remote-session window visible on your Windows PC.
3. Ask Claude to use `list_windows`, then `attach` to that viewer window and `read` the screen.
4. Have Claude `click` into the remote terminal or text field before typing. For a first terminal check, run `hostname` and verify the visible result.

Example prompt:

> Use ScreenLink: list the windows, attach to my AnyDesk remote-session window, read the screen, and tell me what you see. Click into the remote terminal, then run `hostname`. Stop if control is unavailable or ScreenLink reports SAFETY STOP. Never type passwords.

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
| `SCREENLINK_KEY_GAP_MS` | `35` | Pause between typed keys. Raise it if keys get lost on a slow remote connection. |
| `SCREENLINK_OCR_SCALE` | `2` | Upscaling before OCR. Try `3` for tiny fonts. |

## Limits

- OCR can confuse `0`/`O` and `1`/`l`/`|`. The AI is told to double-check values.
- It reads only what's visible, so keep output short (`| tail -n 30`, `grep`).
- Terminal prompts with icons or powerline glyphs read badly. Plain server prompts read well.
- Teams doesn't pass the clipboard through. If the shared picture freezes, ScreenLink waits.
- **It can't stop the client from using their PC.** The checks prevent wrong commands from running, but stray letters can land in the client's window if they keep working.
- Windows only (engineer side). The client side can be any OS supported by the chosen remote tool.

## Project layout

| File | Contents |
|---|---|
| `main.go` | MCP server (JSON-RPC over stdio), tool list and AI instructions. |
| `link.go` | Tools, safety checks, command verification, log. |
| `ocr.go` | Windows OCR worker (PowerShell + `Windows.Media.Ocr`), row rebuilding. |
| `win.go` | Win32: window capture, keyboard and mouse input, kill switch, banner. |
| `match_test.go` | Tests for the "is the typed command really on screen" check. |
