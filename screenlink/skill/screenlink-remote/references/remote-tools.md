# Remote tools: which window to attach to

Use `list_windows` with a filter from the table, then `attach` by `hwnd`. Titles change between versions, so match on the **process** first.

| Tool | Process (filter) | Typical window title | How the engineer gets control | Notes |
|---|---|---|---|---|
| **Microsoft Teams** | `ms-teams` | `Meeting with <name> \| Microsoft Teams`, or a pop-out with the presenter's name | Client shares the screen, then **Give control**; or the engineer uses **Request control** | Keys reach the client only after a click inside the shared screen. The toolbar shows "Stop control" when you have control. |
| **Zoom** | `zoom` | `Zoom Meeting`, or `<name>'s screen` | **View Options → Request Remote Control**, and the client accepts | Maximize the shared-screen window. Prefer side-by-side mode off. |
| **Webex** | `webex`, `CiscoCollabHost` | `Webex` / meeting name | **Request control** from the shared-content menu | Same as Teams: click into the content first. |
| **Google Meet** | `chrome`, `msedge` | Meet tab | **No remote control.** Meet only shares the picture | **Read-only:** use `read` and `look`, and ask the client to type. |
| **AnyDesk** | `AnyDesk` | the remote ID or alias | Client accepts the session with keyboard and mouse allowed | Turn off "Block user input" only if the client agrees. Otherwise the client can still type too. |
| **TeamViewer** | `TeamViewer` | `<ID> - TeamViewer` | Remote control session | Same safety rules: the client must not type at the same time. |
| **Chrome Remote Desktop** | `chrome`, `msedge` | `Chrome Remote Desktop` / host name | Shared session | It's a browser window: attach to the window, not a tab. |
| **RDP** | `mstsc` | `<host> - Remote Desktop Connection` | Direct session | Keep the window visible: an RDP picture may come back black to off-screen capture. RDP is a session of its own, so no client typing conflict. |
| **Citrix** | `CDViewer`, `wfica32` | app or desktop name | Direct session | Keep the window visible. |
| **VMware** | `vmware`, `vmrc` | VM name | VM console | Click inside the console to give it the keyboard; Ctrl+Alt releases it. |
| **Hyper-V** | `vmconnect` | `<VM> on <host> - Virtual Machine Connection` | VM console | Same as VMware. |
| **VirtualBox** | `VirtualBoxVM` | `<VM> [Running] - Oracle VirtualBox` | VM console | The right Ctrl key is the host key: avoid it. |
| **Proxmox / ESXi web console** | `chrome`, `msedge` | noVNC / console tab | VM console in a browser | Click into the console canvas first. |

## Remote tools where the client can also type

Teams, Zoom, Webex, AnyDesk, TeamViewer and Chrome Remote Desktop all let the client keep typing. **If the client types or clicks, our keys follow their focus**, possibly to another display. ScreenLink's exact check before Enter prevents a wrong command from running, but stray letters can still land in the client's window. Always ask the client to keep their hands off.

## Tools with their own session

RDP, Citrix and VM consoles give us our own session, so there's no conflict with the client. Still keep to read-only unless told otherwise.

## If capture is black or empty

- Restore the window (not minimized) and keep it uncovered on the main monitor.
- `read` again. ScreenLink falls back to copying the screen area when the app doesn't draw off-screen.

## If keys don't arrive

1. `status`: check you're attached to the right window and not stopped.
2. `click` inside the remote screen (on the terminal or field).
3. Check that the remote tool still shows that you have control. It can be taken back.
4. If keys still don't arrive, a window on the client side that runs as **Administrator** may be refusing them, or the remote tool may be in view-only mode. Tell the engineer.
