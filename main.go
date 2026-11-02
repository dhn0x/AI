//go:build windows

// screenlink: ScreenLink. A local MCP server for the engineer's PC that
// lets Claude Code / Codex work on a client's screen through a Teams (or any)
// remote-control window, fast: it captures only that window, reads it with
// the Windows OCR engine on this PC and returns TEXT, and types commands as
// real key presses. Nothing is installed on the client.
//
//	claude mcp add screenlink -- C:\path\to\screenlink.exe
//
// Safety: it only acts while the attached window is in front; Ctrl+Alt+Q stops
// it at once (press again to allow it again; the AI cannot); risky commands
// need a Yes from the engineer; it never types at a password prompt; every
// action is logged under the engineer's Windows name.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

const version = "0.1.2"

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type server struct {
	omu sync.Mutex
	out *os.File
	sl  *link
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "windows":
			for _, w := range listWindows() {
				fmt.Printf("%-10d %-22s %5dx%-5d %s\n", w.HWND, w.Process, w.Width, w.Height, w.Title)
			}
			return
		case "version", "--version":
			fmt.Println("screenlink", version)
			return
		case "mcp":
		default:
			fmt.Fprintln(os.Stderr, "usage: screenlink [mcp|windows|version]   (mcp is the default: run it from Claude Code / Codex)")
			os.Exit(2)
		}
	}
	sl, err := newLink()
	if err != nil {
		fmt.Fprintln(os.Stderr, "screenlink:", err)
		os.Exit(1)
	}
	defer sl.close()
	s := &server{out: os.Stdout, sl: sl}
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	for {
		ln, err := in.ReadBytes('\n')
		if len(strings.TrimSpace(string(ln))) > 0 {
			var r rpcReq
			if json.Unmarshal(ln, &r) != nil {
				s.reply(nil, nil, &rpcErr{-32700, "parse error"})
			} else {
				go s.handle(r)
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *server) reply(id json.RawMessage, result any, e *rpcErr) {
	if id == nil && e == nil {
		return
	}
	msg := map[string]any{"jsonrpc": "2.0", "id": id}
	if e != nil {
		msg["error"] = e
	} else {
		msg["result"] = result
	}
	b, _ := json.Marshal(msg)
	s.omu.Lock()
	defer s.omu.Unlock()
	_, _ = s.out.Write(append(b, '\n'))
}

const instructions = "ScreenLink: work on a client's computer through a Teams (or other) remote-control window on " +
	"this PC. 1) list_windows, then attach to the window that shows the client's screen. 2) Prefer TEXT: use read " +
	"(OCR of the window) and only use look (image) when you must see graphics. 3) For command-line work, have a " +
	"terminal open on the client's screen, click into it once with click (by text such as the prompt, or x,y; ScreenLink re-clicks there whenever it has to bring the window back to the front, because Teams passes keys only while the shared screen has focus), then use run(command): it types the command, waits " +
	"until the screen settles and returns the new output as text. Keep output short (| tail -n 40, | head, grep, " +
	"Select-Object -Last 40) because OCR reads only what is visible; ask for a larger terminal font if text is misread. " +
	"4) Never type passwords: when a password prompt appears, ask the engineer to type it. Risky commands pop up a " +
	"Yes/No box for the engineer. If you get 'stopped', the engineer pressed Ctrl+Alt+Q: stop and ask them. " +
	"OCR can misread characters (0/O, 1/l, quotes); double-check before acting on exact values. The client was told an " +
	"AI assistant is helping; be careful and explain what you are about to do."

func (s *server) handle(r rpcReq) {
	switch r.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(r.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		s.reply(r.ID, map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "screenlink", "version": version},
			"instructions":    instructions,
		}, nil)
	case "ping":
		s.reply(r.ID, map[string]any{}, nil)
	case "tools/list":
		s.reply(r.ID, map[string]any{"tools": toolDefs}, nil)
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(r.Params, &p); err != nil {
			s.reply(r.ID, nil, &rpcErr{-32602, "invalid params"})
			return
		}
		content, err := s.sl.call(p.Name, p.Arguments)
		res := map[string]any{"content": content}
		if err != nil {
			res["content"] = []map[string]any{{"type": "text", "text": "Error: " + err.Error()}}
			res["isError"] = true
		}
		s.reply(r.ID, res, nil)
	default:
		if r.ID != nil {
			s.reply(r.ID, nil, &rpcErr{-32601, "method not found: " + r.Method})
		}
	}
}

func obj(props map[string]any, required ...string) map[string]any {
	o := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		o["required"] = required
	}
	return o
}

func str(d string) map[string]any { return map[string]any{"type": "string", "description": d} }
func num(d string) map[string]any { return map[string]any{"type": "integer", "description": d} }
func boo(d string) map[string]any { return map[string]any{"type": "boolean", "description": d} }
func tool(n, d string, schema map[string]any) map[string]any {
	return map[string]any{"name": n, "description": d, "inputSchema": schema}
}

var regionArg = str("optional part of the window to use: \"x,y,width,height\" in window pixels (see read with boxes=true)")

var toolDefs = []map[string]any{
	tool("list_windows", "List visible windows on this PC (Teams meeting / screen-share windows, RDP, etc.) to attach to.",
		obj(map[string]any{"filter": str("optional text to match in the title or process name, e.g. 'teams'")})),
	tool("attach", "Attach to the window that shows the client's screen. Returns its size and the text on it.",
		obj(map[string]any{"hwnd": num("window handle from list_windows"), "title": str("or: text in the window title")})),
	tool("status", "Attached window, whether the engineer has stopped ScreenLink (Ctrl+Alt+Q), and the log file.", obj(map[string]any{})),
	tool("read", "Read the attached window as text (local OCR, fast). Use this instead of images whenever possible.",
		obj(map[string]any{"region": regionArg, "boxes": boo("include each line's x,y,w,h (for click or region)"),
			"changes_only": boo("only lines that are new since the previous read")})),
	tool("look", "Small image of the window (or a region) for when you must see graphics (charts, icons, colours). Slower than read.",
		obj(map[string]any{"region": regionArg, "max_width": num("default 1280")})),
	tool("run", "Type a command into the terminal that has focus on the client's screen, press Enter, wait until the screen settles and return the new output as text. Click into the terminal first.",
		obj(map[string]any{"command": str("the command line"), "timeout_s": num("max seconds to wait (default 60)"),
			"idle_ms": num("screen must be still this long to count as finished (default 1500)"), "region": regionArg}, "command")),
	tool("type", "Type text as key presses into the focused field on the client's screen (no passwords).",
		obj(map[string]any{"text": str("text to type"), "enter": boo("press Enter after")}, "text")),
	tool("keys", "Press keys or shortcuts, e.g. \"enter\", \"ctrl+c\", \"tab tab enter\", \"alt+f4\", \"up\", \"f5\", \"ctrl+shift+esc\".",
		obj(map[string]any{"keys": str("space-separated keys; combine with +")}, "keys")),
	tool("click", "Click on the client's screen: by visible text (OCR) or by window x,y.",
		obj(map[string]any{"text": str("click the first line/word containing this text"), "x": num("window x"), "y": num("window y"),
			"right": boo("right-click"), "double": boo("double-click"), "nth": num("if the text appears more than once, which one (1 = first)")})),
	tool("scroll", "Scroll the mouse wheel over the window (or at x,y).",
		obj(map[string]any{"direction": str("up or down"), "amount": num("wheel clicks (default 5)"), "x": num("window x"), "y": num("window y")}, "direction")),
	tool("wait", "Wait until the screen has been still for idle_ms (e.g. a long job finished), then read it.",
		obj(map[string]any{"timeout_s": num("max seconds (default 120)"), "idle_ms": num("default 3000"), "region": regionArg})),
}
