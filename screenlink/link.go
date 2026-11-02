//go:build windows

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

type link struct {
	mu      sync.Mutex // one action at a time
	hwnd    uintptr
	title   string
	stopped atomic.Bool
	ocr     *ocrWorker
	ui      *ui
	logf    *os.File
	logPath string
	user    string
	last    []line // previous read (changes_only)
	anchor  *point // where the AI last clicked (window coords): clicked again to give the shared screen keyboard focus
}

func newLink() (*link, error) {
	o, err := newOCR()
	if err != nil {
		return nil, err
	}
	l := &link{ocr: o, user: os.Getenv("USERDOMAIN") + `\` + os.Getenv("USERNAME")}
	dir := filepath.Join(os.Getenv("LOCALAPPDATA"), "ScreenLink", "logs")
	if err := os.MkdirAll(dir, 0o700); err == nil {
		l.logPath = filepath.Join(dir, time.Now().Format("2006-01-02")+".jsonl")
		l.logf, _ = os.OpenFile(l.logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	}
	l.ui = startUI(func() bool {
		now := !l.stopped.Load()
		l.stopped.Store(now)
		l.log("kill-switch", map[string]any{"stopped": now}, "")
		l.refreshBanner()
		return now
	})
	return l, nil
}

func (l *link) close() {
	l.ui.setBanner("")
	l.ocr.close()
	if l.logf != nil {
		l.logf.Close()
	}
}

func (l *link) refreshBanner() {
	switch {
	case l.hwnd == 0:
		l.ui.setBanner("")
	case l.stopped.Load():
		l.ui.setBanner("ScreenLink: STOPPED  (Ctrl+Alt+Q to allow AI again)")
	default:
		l.ui.setBanner("ScreenLink: AI is working on: " + trunc(l.title, 40) + "   |   Ctrl+Alt+Q = STOP")
	}
}

func (l *link) log(action string, args any, result string) {
	if l.logf == nil {
		return
	}
	b, _ := json.Marshal(map[string]any{"time": time.Now().Format(time.RFC3339), "engineer": l.user, "window": l.title,
		"action": action, "args": args, "result": trunc(result, 2000)})
	l.logf.Write(append(b, '\n'))
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

type args struct {
	Filter      string `json:"filter,omitempty"`
	HWND        uint64 `json:"hwnd,omitempty"`
	Title       string `json:"title,omitempty"`
	Region      string `json:"region,omitempty"`
	Boxes       bool   `json:"boxes,omitempty"`
	ChangesOnly bool   `json:"changes_only,omitempty"`
	MaxWidth    int    `json:"max_width,omitempty"`
	Command     string `json:"command,omitempty"`
	TimeoutS    int    `json:"timeout_s,omitempty"`
	IdleMS      int    `json:"idle_ms,omitempty"`
	Text        string `json:"text,omitempty"`
	Enter       bool   `json:"enter,omitempty"`
	Keys        string `json:"keys,omitempty"`
	X           *int   `json:"x,omitempty"`
	Y           *int   `json:"y,omitempty"`
	Right       bool   `json:"right,omitempty"`
	Double      bool   `json:"double,omitempty"`
	Nth         int    `json:"nth,omitempty"`
	Direction   string `json:"direction,omitempty"`
	Amount      int    `json:"amount,omitempty"`
}

func text(s string) []map[string]any { return []map[string]any{{"type": "text", "text": s}} }

func (l *link) call(name string, raw json.RawMessage) ([]map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var a args
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &a); err != nil {
			return nil, fmt.Errorf("bad arguments: %w", err)
		}
	}
	var argsLog any = a
	if name == "type" {
		argsLog = map[string]any{"chars": len(a.Text), "enter": a.Enter} // never log typed text verbatim
	}
	out, err := l.do(name, a)
	res := ""
	if err != nil {
		res = "error: " + err.Error()
	} else if len(out) > 0 {
		if t, ok := out[0]["text"].(string); ok {
			res = t
		}
	}
	if name != "status" && name != "list_windows" {
		l.log(name, argsLog, res)
	}
	return out, err
}

func (l *link) do(name string, a args) ([]map[string]any, error) {
	filter, hwnd, title, region, boxes, changesOnly, maxWidth := a.Filter, a.HWND, a.Title, a.Region, a.Boxes, a.ChangesOnly, a.MaxWidth
	command, timeoutS, idleMS, txt, enter, keys := a.Command, a.TimeoutS, a.IdleMS, a.Text, a.Enter, a.Keys
	x, y, right, double, nth, direction, amount := a.X, a.Y, a.Right, a.Double, a.Nth, a.Direction, a.Amount

	switch name {
	case "list_windows":
		var b strings.Builder
		f := strings.ToLower(filter)
		for _, w := range listWindows() {
			if f != "" && !strings.Contains(strings.ToLower(w.Title+" "+w.Process), f) {
				continue
			}
			fmt.Fprintf(&b, "hwnd=%d  %s  %dx%d  %q\n", w.HWND, w.Process, w.Width, w.Height, w.Title)
		}
		if b.Len() == 0 {
			return text("no matching windows"), nil
		}
		return text(b.String() + "\nTeams: the client's shared screen is usually in the meeting window or a pop-out titled with the presenter's name."), nil

	case "attach":
		var found *winInfo
		for _, w := range listWindows() {
			w := w
			if (hwnd != 0 && uint64(w.HWND) == hwnd) || (hwnd == 0 && title != "" && strings.Contains(strings.ToLower(w.Title), strings.ToLower(title))) {
				found = &w
				break
			}
		}
		if found == nil {
			return nil, errors.New("window not found: use list_windows")
		}
		l.hwnd, l.title, l.last, l.anchor = found.HWND, found.Title, nil, nil
		l.refreshBanner()
		lines, err := l.readLines("")
		if err != nil {
			return nil, err
		}
		return text(fmt.Sprintf("attached to %q (%s, %dx%d)\n--- screen text ---\n%s", found.Title, found.Process, found.Width, found.Height, joinLines(lines, false))), nil

	case "status":
		st := "running"
		if l.stopped.Load() {
			st = "STOPPED by the engineer (Ctrl+Alt+Q)"
		}
		w := "none"
		if l.hwnd != 0 {
			w = fmt.Sprintf("%q (hwnd %d)", l.title, l.hwnd)
		}
		return text(fmt.Sprintf("ScreenLink %s\nwindow: %s\nstate: %s\nengineer: %s\nlog: %s", version, w, st, l.user, l.logPath)), nil

	case "read":
		lines, err := l.readLines(region)
		if err != nil {
			return nil, err
		}
		show := lines
		if changesOnly && l.last != nil {
			show = newLines(l.last, lines)
		}
		l.last = lines
		if len(show) == 0 {
			return text("(no text found)"), nil
		}
		return text(joinLines(show, boxes)), nil

	case "look":
		img, err := l.grab(region)
		if err != nil {
			return nil, err
		}
		if maxWidth <= 0 {
			maxWidth = 1280
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, shrink(img, maxWidth)); err != nil {
			return nil, err
		}
		b := img.Bounds()
		return []map[string]any{
			{"type": "image", "data": base64.StdEncoding.EncodeToString(buf.Bytes()), "mimeType": "image/png"},
			{"type": "text", "text": fmt.Sprintf("window region %d,%d %dx%d (image scaled to max %dpx wide; give click/region in window pixels)", b.Min.X, b.Min.Y, b.Dx(), b.Dy(), maxWidth)},
		}, nil

	case "run":
		if strings.TrimSpace(command) == "" {
			return nil, errors.New("empty command")
		}
		if err := l.ready(); err != nil {
			return nil, err
		}
		if err := l.noPasswordPrompt(region); err != nil {
			return nil, err
		}
		if risky.MatchString(command) && !confirm("ScreenLink: approve command?",
			"The AI wants to run this on the client's screen:\n\n"+command+"\n\nAllow it?") {
			return nil, errors.New("the engineer said No to this command")
		}
		if err := l.focusTarget(); err != nil { // also after the confirm box took focus
			return nil, err
		}
		before, _ := l.readLines(region)
		if err := l.typeChecked(command, region, before); err != nil {
			return nil, err
		}
		if timeoutS <= 0 {
			timeoutS = 60
		}
		if idleMS <= 0 {
			idleMS = 1500
		}
		done := l.waitIdle(region, time.Duration(timeoutS)*time.Second, time.Duration(idleMS)*time.Millisecond, 400*time.Millisecond)
		after, err := l.readLines(region)
		if err != nil {
			return nil, err
		}
		l.last = after
		out := outputAfter(command, before, after)
		note := ""
		if !done {
			note = fmt.Sprintf("\n[still changing after %ds: probably still running; use wait or read]", timeoutS)
		}
		return text(out + note), nil

	case "type":
		if err := l.focusTarget(); err != nil {
			return nil, err
		}
		if err := l.noPasswordPrompt(""); err != nil {
			return nil, err
		}
		if enter && strings.TrimSpace(txt) != "" {
			before, _ := l.readLines("")
			if err := l.typeChecked(txt, "", before); err != nil {
				return nil, err
			}
		} else if err := l.typeText(txt); err != nil {
			return nil, err
		}
		return text(fmt.Sprintf("typed %d characters", len([]rune(txt)))), nil

	case "keys":
		if err := l.ready(); err != nil {
			return nil, err
		}
		if strings.Contains(strings.ToLower(strings.ReplaceAll(keys, " ", "")), "ctrl+alt+q") {
			return nil, errors.New("Ctrl+Alt+Q is the engineer's kill switch; it cannot be pressed by the AI")
		}
		for _, combo := range strings.Fields(keys) {
			if l.stopped.Load() {
				return nil, errStopped
			}
			if err := pressCombo(combo); err != nil {
				return nil, err
			}
			time.Sleep(60 * time.Millisecond)
		}
		return text("pressed " + keys), nil

	case "click":
		if err := l.ready(); err != nil {
			return nil, err
		}
		cx, cy := 0, 0
		switch {
		case txt != "":
			lines, err := l.readLines("")
			if err != nil {
				return nil, err
			}
			px, py, ok := findText(lines, txt, nth)
			if !ok {
				return nil, fmt.Errorf("%q not found on screen (OCR): try read with boxes=true and click by x,y", txt)
			}
			cx, cy = px, py
		case x != nil && y != nil:
			cx, cy = *x, *y
		default:
			return nil, errors.New("give text, or x and y")
		}
		w, h := clientSize(l.hwnd)
		if cx < 0 || cy < 0 || cx >= w || cy >= h {
			return nil, fmt.Errorf("%d,%d is outside the window (%dx%d)", cx, cy, w, h)
		}
		o := clientOrigin(l.hwnd)
		clickAt(int(o.X)+cx, int(o.Y)+cy, right, double)
		if !right {
			l.anchor = &point{int32(cx), int32(cy)}
		}
		return text(fmt.Sprintf("clicked at window %d,%d", cx, cy)), nil

	case "scroll":
		if err := l.ready(); err != nil {
			return nil, err
		}
		if amount <= 0 {
			amount = 5
		}
		w, h := clientSize(l.hwnd)
		cx, cy := w/2, h/2
		if x != nil && y != nil {
			cx, cy = *x, *y
		}
		n := amount
		if strings.EqualFold(direction, "down") {
			n = -amount
		}
		o := clientOrigin(l.hwnd)
		wheelAt(int(o.X)+cx, int(o.Y)+cy, n)
		time.Sleep(400 * time.Millisecond)
		return text("scrolled " + direction), nil

	case "wait":
		if l.hwnd == 0 {
			return nil, errors.New("not attached: use list_windows and attach")
		}
		if timeoutS <= 0 {
			timeoutS = 120
		}
		if idleMS <= 0 {
			idleMS = 3000
		}
		done := l.waitIdle(region, time.Duration(timeoutS)*time.Second, time.Duration(idleMS)*time.Millisecond, 0)
		lines, err := l.readLines(region)
		if err != nil {
			return nil, err
		}
		l.last = lines
		s := joinLines(lines, false)
		if !done {
			s += fmt.Sprintf("\n[still changing after %ds]", timeoutS)
		}
		return text(s), nil
	}
	return nil, fmt.Errorf("unknown tool %q", name)
}

// ocrScale upscales before OCR; 2x with smoothing read best in tests.
var ocrScale = envInt("SCREENLINK_OCR_SCALE", 2)

func envInt(k string, d int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil && n > 0 {
		return n
	}
	return d
}

// keyGap: pause between typed keys (SCREENLINK_KEY_GAP_MS), so the Teams
// remote-control link keeps up.
var keyGap = time.Duration(envInt("SCREENLINK_KEY_GAP_MS", 35)) * time.Millisecond

var errStopped = errors.New("stopped: the engineer pressed Ctrl+Alt+Q. Do not continue; ask the engineer")

// ready checks the kill switch and brings the attached window to the front;
// nothing is typed or clicked anywhere else.
func (l *link) ready() error {
	if l.stopped.Load() {
		return errStopped
	}
	if l.hwnd == 0 {
		return errors.New("not attached: use list_windows and attach")
	}
	if !isWindow(l.hwnd) {
		l.hwnd = 0
		l.refreshBanner()
		return errors.New("the attached window closed (meeting ended or control taken back?)")
	}
	if !focus(l.hwnd) {
		return errors.New("could not bring the attached window to the front; ask the engineer to click on it")
	}
	return nil
}

// focusTarget makes sure keys go to the right place on the CLIENT's side too:
// Teams sends keys to whatever window has focus on the client's PC, which can
// be on a second display we cannot see. So before typing, always click where
// the AI last clicked (the client's terminal on the shared screen).
func (l *link) focusTarget() error {
	if err := l.ready(); err != nil {
		return err
	}
	if l.anchor == nil {
		return errors.New("first click into the client's terminal or field on the shared screen (click with text or x,y), so keys cannot land in a window on another display")
	}
	o := clientOrigin(l.hwnd)
	clickAt(int(o.X)+int(l.anchor.X), int(o.Y)+int(l.anchor.Y), false, false)
	time.Sleep(200 * time.Millisecond)
	return nil
}

// typeChecked types text WITHOUT Enter and presses Enter only when the line
// on the shared screen shows EXACTLY that text (OCR look-alikes allowed):
//   - not visible at all → the keys went elsewhere (e.g. the client's other
//     display): stop, Enter not pressed;
//   - visible but different (usually the client clicked into another window
//     while we typed, so some keys went there) → click back into our
//     terminal, erase, type once more; still different → erase and stop.
//
// After Enter it checks the screen reacted and presses Enter once more if the
// Enter itself was dropped (an extra Enter only adds an empty prompt).
func (l *link) typeChecked(cmd, region string, before []line) error {
	for attempt := 1; ; attempt++ {
		if err := l.typeText(cmd); err != nil {
			return err
		}
		got, ok, seen, err := l.echoed(cmd, region, before)
		if err != nil {
			return err
		}
		if ok {
			break
		}
		if !seen {
			return errors.New("SAFETY STOP: the typed text did not appear on the shared screen, so Enter was NOT pressed. " +
				"The keys may have gone to another window on the client's PC (e.g. on a second display). Ask the engineer/client to " +
				"check and clear any stray text there; then click into the terminal again and retry")
		}
		// Part of the text is missing: usually the client clicked into another
		// window (e.g. on their second display) while we typed. Click back into
		// our terminal BEFORE erasing, so backspaces cannot hit the client's work.
		if err := l.focusTarget(); err != nil {
			return err
		}
		if err := l.erase(len([]rune(cmd)) + 2); err != nil {
			return err
		}
		if attempt == 2 {
			return fmt.Errorf("SAFETY STOP: the line on screen read %q instead of exactly %q, so Enter was NOT pressed and the line was erased. "+
				"Most likely the client is using their PC (e.g. their other display) at the same time, so some keys went there. "+
				"Ask the engineer to have the client stop using mouse/keyboard while we work, and to check for stray text on their other display", got, cmd)
		}
		time.Sleep(500 * time.Millisecond)
		if err := l.focusTarget(); err != nil {
			return err
		}
	}
	if l.stopped.Load() {
		return errStopped
	}
	if foreground() != l.hwnd {
		return errors.New("the window lost focus before Enter; nothing was run")
	}
	// Click into our terminal right before Enter too, so an Enter can't land
	// in a window the client is using on another display.
	if err := l.focusTarget(); err != nil {
		return err
	}
	img, _ := l.grab(region)
	tap(0x0D)
	if img != nil && !l.reacted(region, gray(img, 4), 3*time.Second) {
		if l.stopped.Load() || foreground() != l.hwnd {
			return errors.New("Enter may not have arrived and the window lost focus; read the screen before doing anything else")
		}
		if err := l.focusTarget(); err != nil {
			return err
		}
		tap(0x0D) // the Enter did not arrive
	}
	return nil
}

// echoed polls the screen (the Teams picture lags) for the typed line.
// ok: a new line ends with exactly cmd; seen: something like cmd is visible.
func (l *link) echoed(cmd, region string, before []line) (got string, ok, seen bool, err error) {
	for try := 0; try < 5; try++ {
		time.Sleep(time.Duration(400+250*try) * time.Millisecond)
		after, err := l.readLines(region)
		if err != nil {
			return "", false, false, err
		}
		best, bestScore := line{}, 0.0
		for _, r := range newLines(before, after) {
			if sc := similar(cmd, r.Text); sc > bestScore {
				best, bestScore = r, sc
			}
		}
		if bestScore < 0.4 {
			continue
		}
		seen, got = true, best.Text
		if typedOK(best.Text, cmd) {
			return got, true, true, nil
		}
		// Close but not exact: read just that row bigger before deciding.
		if bestScore >= 0.85 {
			if row, err := l.grab(fmt.Sprintf("0,%d,%d,%d", max(best.Y-6, 0), 1<<20, best.H+12)); err == nil {
				if ls, err := l.ocr.recognize(row, 4); err == nil {
					for _, r := range ls {
						if typedOK(r.Text, cmd) {
							return r.Text, true, true, nil
						}
					}
				}
			}
		}
	}
	return got, false, seen, nil
}

// typedOK decides whether the row on screen shows the whole command. When the
// client takes focus while we type, keys go missing as one block (usually the
// end); OCR mistakes are single scattered characters. So:
//   - short or risky commands must appear exactly (OCR look-alikes folded);
//   - longer ones need their start and end present and >= 85% of the text.
//
// "Contains" rather than "ends with": PowerShell shows a grey suggestion after
// the typed text, which Enter does not accept.
func typedOK(row, cmd string) bool {
	r, c := loose(row), loose(cmd)
	if c == "" {
		return false
	}
	if strings.Contains(r, c) {
		return true
	}
	if len(c) <= 12 || risky.MatchString(cmd) {
		return false
	}
	head, tail := c[:6], c[len(c)-6:]
	return similar(cmd, row) >= 0.85 && fuzzyContains(r, head, 1) && fuzzyContains(r, tail, 1)
}

// loose: norm without "|", which OCR often drops or reads as a column gap.
func loose(s string) string { return norm(strings.ReplaceAll(s, "|", "")) }

// fuzzyContains: some stretch of s matches sub with at most miss characters missing.
func fuzzyContains(s, sub string, miss int) bool {
	rs, rsub := []rune(s), []rune(sub)
	for i := 0; i < len(rs); i++ {
		j, k, missed := i, 0, 0
		for k < len(rsub) && j < len(rs) && j < i+len(rsub)+miss {
			if rs[j] == rsub[k] {
				j++
				k++
			} else if missed < miss {
				missed++
				k++ // skip a character of sub (dropped by OCR)
			} else {
				break
			}
		}
		if len(rsub)-k+missed <= miss && k >= len(rsub)-miss {
			return true
		}
	}
	return false
}

// erase deletes n characters before the cursor (works in any shell).
func (l *link) erase(n int) error {
	for i := 0; i < n; i++ {
		if foreground() != l.hwnd {
			return errors.New("the window lost focus while erasing; check the client's prompt")
		}
		tap(0x08)
		time.Sleep(keyGap)
	}
	return nil
}

// reacted: did the screen change within d (compared with a frame from before)?
func (l *link) reacted(region string, prev []byte, d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		time.Sleep(300 * time.Millisecond)
		img, err := l.grab(region)
		if err != nil {
			return true
		}
		if changed(prev, gray(img, 4)) {
			return true
		}
	}
	return false
}

// similar: how much of cmd (OCR-normalised, first 40 characters) appears in
// order within row, 0..1 (longest common subsequence).
func similar(cmd, row string) float64 {
	a, b := []rune(norm(cmd)), []rune(norm(row))
	if len(a) > 40 {
		a = a[:40]
	}
	if len(a) == 0 {
		return 0
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				cur[j] = prev[j-1] + 1
			} else {
				cur[j] = max(cur[j-1], prev[j])
			}
		}
		prev, cur = cur, prev
	}
	return float64(prev[len(b)]) / float64(len(a))
}

func (l *link) typeText(s string) error {
	for _, r := range s {
		if l.stopped.Load() {
			return errStopped
		}
		if foreground() != l.hwnd {
			return errors.New("stopped typing: the attached window lost focus (someone clicked elsewhere)")
		}
		typeRune(r)
		time.Sleep(keyGap) // Teams remote control drops keys sent faster
	}
	return nil
}

var passwordPrompt = regexp.MustCompile(`(?i)(password|passphrase|passcode|\bPIN\b)[^\n]{0,30}:\s*\S?\s*$`)

// noPasswordPrompt refuses to type when the line the cursor waits on asks for a secret.
func (l *link) noPasswordPrompt(region string) error {
	err := l.passwordOnScreen(region)
	if err != nil { // the screen may still be redrawing (e.g. just after Ctrl+C): look again once
		time.Sleep(1500 * time.Millisecond)
		err = l.passwordOnScreen(region)
	}
	return err
}

func (l *link) passwordOnScreen(region string) error {
	lines, err := l.readLines(region)
	if err != nil || len(lines) == 0 {
		return nil
	}
	for i := len(lines) - 1; i >= 0; i-- { // the last real line (skip cursor / icon specks)
		letters := 0
		for _, r := range lines[i].Text {
			if unicode.IsLetter(r) {
				letters++
			}
		}
		if letters < 2 {
			continue
		}
		if passwordPrompt.MatchString(lines[i].Text) {
			return fmt.Errorf("a password prompt is on screen (%q): the engineer or client must type it; then continue", trunc(lines[i].Text, 80))
		}
		break
	}
	return nil
}

var risky = regexp.MustCompile(`(?i)(\brm\s+-[a-z]*[rf]|\brmdir\b|\bdel\b|\berase\b|remove-item|\bformat\s+[a-z]:|format-volume|clear-disk|\bmkfs|\bdd\s+if=|` +
	`shutdown|reboot|restart-computer|stop-computer|\binit\s+[06]\b|\bhalt\b|poweroff|` +
	`systemctl\s+(stop|restart|disable|mask|kill)|\bservice\s+\S+\s+(stop|restart)|stop-service|restart-service|set-service|sc(\.exe)?\s+(stop|delete|config)|` +
	`\bkill(all)?\b|pkill|stop-process|taskkill|` +
	`drop\s+(table|database)|truncate|delete\s+from|` +
	`(yum|dnf|apt|apt-get|zypper|rpm|pip)\s+(remove|erase|purge|-e)|uninstall|msiexec|` +
	`\bchmod\s+-R|\bchown\s+-R|\bmv\s|move-item|>\s*/etc/|iptables|firewall-cmd|netsh|new-netfirewall|set-netfirewall|` +
	`userdel|passwd|net\s+user|reg\s+(add|delete)|set-itemproperty|crontab\s+-r)`)

func (l *link) grab(region string) (*image.RGBA, error) {
	if l.hwnd == 0 {
		return nil, errors.New("not attached: use list_windows and attach")
	}
	img, err := capture(l.hwnd)
	if err != nil {
		return nil, err
	}
	if region == "" {
		return img, nil
	}
	var v [4]int
	parts := strings.Split(region, ",")
	if len(parts) != 4 {
		return nil, errors.New(`region must be "x,y,width,height"`)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, errors.New(`region must be "x,y,width,height"`)
		}
		v[i] = n
	}
	r := image.Rect(v[0], v[1], v[0]+v[2], v[1]+v[3]).Intersect(img.Bounds())
	if r.Empty() {
		return nil, errors.New("region is outside the window")
	}
	return img.SubImage(r).(*image.RGBA), nil
}

func (l *link) readLines(region string) ([]line, error) {
	img, err := l.grab(region)
	if err != nil {
		return nil, err
	}
	lines, err := l.ocr.recognize(img, ocrScale)
	if err != nil {
		return nil, err
	}
	off := img.Bounds().Min
	for i := range lines { // back to window coordinates
		lines[i].X += off.X
		lines[i].Y += off.Y
		for j := range lines[i].Words {
			lines[i].Words[j].X += off.X
			lines[i].Words[j].Y += off.Y
		}
	}
	return lines, nil
}

// waitIdle polls small grayscale frames until the screen has been still for
// idle (tiny changes such as a blinking cursor or video noise are ignored).
func (l *link) waitIdle(region string, timeout, idle, minWait time.Duration) bool {
	start := time.Now()
	deadline := start.Add(timeout)
	var prev []byte
	stillSince := time.Now()
	for time.Now().Before(deadline) {
		img, err := l.grab(region)
		if err != nil {
			return false
		}
		g := gray(img, 4)
		if prev == nil || changed(prev, g) {
			stillSince = time.Now()
		}
		prev = g
		if time.Since(start) >= minWait && time.Since(stillSince) >= idle {
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

func gray(img *image.RGBA, step int) []byte {
	b := img.Bounds()
	out := make([]byte, 0, (b.Dx()/step+1)*(b.Dy()/step+1))
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			i := img.PixOffset(x, y)
			out = append(out, byte((int(img.Pix[i])*3+int(img.Pix[i+1])*6+int(img.Pix[i+2]))/10))
		}
	}
	return out
}

func changed(a, b []byte) bool {
	if len(a) != len(b) {
		return true
	}
	n := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d > 32 || d < -32 {
			n++
		}
	}
	return n*1000 > len(a)*2 // more than 0.2% of pixels changed
}

func shrink(img *image.RGBA, maxW int) image.Image {
	b := img.Bounds()
	if b.Dx() <= maxW {
		return img
	}
	h := b.Dy() * maxW / b.Dx()
	out := image.NewRGBA(image.Rect(0, 0, maxW, h))
	for y := 0; y < h; y++ {
		sy := b.Min.Y + y*b.Dy()/h
		for x := 0; x < maxW; x++ {
			sx := b.Min.X + x*b.Dx()/maxW
			copy(out.Pix[out.PixOffset(x, y):out.PixOffset(x, y)+4], img.Pix[img.PixOffset(sx, sy):img.PixOffset(sx, sy)+4])
		}
	}
	return out
}

func joinLines(lines []line, boxes bool) string {
	var b strings.Builder
	for _, l := range lines {
		if boxes {
			fmt.Fprintf(&b, "[%d,%d %dx%d] ", l.X, l.Y, l.W, l.H)
		}
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

// norm lower-cases, drops spaces and folds characters OCR mixes up
// (| I l 1 → i, 0 → o, ; → ,), for matching only.
func norm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsSpace(r):
		case r == '|' || r == 'l' || r == '1' || r == '!':
			b.WriteRune('i')
		case r == '0':
			b.WriteRune('o')
		case r == ';':
			b.WriteRune(',')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// newLines: lines in after that were not in before (multiset by normalised text).
func newLines(before, after []line) []line {
	seen := map[string]int{}
	for _, l := range before {
		seen[norm(l.Text)]++
	}
	var out []line
	for _, l := range after {
		k := norm(l.Text)
		if seen[k] > 0 {
			seen[k]--
			continue
		}
		out = append(out, l)
	}
	return out
}

// outputAfter returns what the command printed: the lines after the last line
// that shows the command (OCR-tolerant), or else the lines that are new.
func outputAfter(command string, before, after []line) string {
	for i := len(after) - 1; i >= 0; i-- {
		if similar(command, after[i].Text) >= 0.7 {
			out := joinLines(after[i+1:], false)
			if out == "" {
				out = "(no output yet: the prompt may still be busy; use wait or read)"
			}
			return out
		}
	}
	nl := newLines(before, after)
	if len(nl) == 0 {
		return "(no change on screen: is the terminal focused? click into it, then try again)\n--- screen ---\n" + joinLines(after, false)
	}
	return "(command line not recognised by OCR; new lines on screen:)\n" + joinLines(nl, false)
}

// findText returns the centre of the nth word/line containing s.
func findText(lines []line, s string, nth int) (int, int, bool) {
	if nth < 1 {
		nth = 1
	}
	want := norm(s)
	n := 0
	for _, l := range lines {
		if !strings.Contains(norm(l.Text), want) {
			continue
		}
		n++
		if n < nth {
			continue
		}
		for _, w := range l.Words { // single word match: click that word
			if strings.Contains(norm(w.Text), want) {
				return w.X + w.W/2, w.Y + w.H/2, true
			}
		}
		// phrase across words: centre of the words that form it
		for i := range l.Words {
			acc := ""
			for j := i; j < len(l.Words); j++ {
				acc += norm(l.Words[j].Text)
				if strings.Contains(acc, want) {
					a, z := l.Words[i], l.Words[j]
					return (a.X + z.X + z.W) / 2, a.Y + a.H/2, true
				}
				if len(acc) > len(want)+20 {
					break
				}
			}
		}
		return l.X + l.W/2, l.Y + l.H/2, true
	}
	return 0, 0, false
}

var namedKeys = map[string]uint16{
	"enter": 0x0D, "return": 0x0D, "tab": 0x09, "esc": 0x1B, "escape": 0x1B, "space": 0x20, "backspace": 0x08, "bksp": 0x08,
	"delete": 0x2E, "del": 0x2E, "insert": 0x2D, "home": 0x24, "end": 0x23, "pageup": 0x21, "pgup": 0x21, "pagedown": 0x22, "pgdn": 0x22,
	"up": 0x26, "down": 0x28, "left": 0x25, "right": 0x27,
	"ctrl": 0x11, "control": 0x11, "shift": 0x10, "alt": 0x12, "win": 0x5B,
}

func pressCombo(combo string) error {
	var vks []uint16
	for _, k := range strings.Split(strings.ToLower(combo), "+") {
		switch {
		case namedKeys[k] != 0:
			vks = append(vks, namedKeys[k])
		case len(k) >= 2 && k[0] == 'f':
			n, err := strconv.Atoi(k[1:])
			if err != nil || n < 1 || n > 12 {
				return fmt.Errorf("unknown key %q", k)
			}
			vks = append(vks, uint16(0x70+n-1))
		case len(k) == 1 && (k[0] >= 'a' && k[0] <= 'z' || k[0] >= '0' && k[0] <= '9'):
			vks = append(vks, uint16(strings.ToUpper(k)[0]))
		default:
			return fmt.Errorf("unknown key %q", k)
		}
	}
	if len(vks) == 1 && vks[0] == 0x5B {
		return errors.New("the Windows key alone is not allowed")
	}
	for _, v := range vks {
		vkDown(v)
		time.Sleep(keyHold)
	}
	for i := len(vks) - 1; i >= 0; i-- {
		vkUp(vks[i])
	}
	return nil
}
