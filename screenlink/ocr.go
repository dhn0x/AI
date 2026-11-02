//go:build windows

package main

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unicode"
	"unicode/utf8"
)

// OCR uses the Windows built-in engine (Windows.Media.Ocr) through one
// long-running PowerShell worker, so each read costs ~100-300 ms instead of a
// PowerShell start-up. Images go over as BMP files in a private temp folder.

const ocrScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
function Await($op, [Type]$t) { $task = $asTask.MakeGenericMethod($t).Invoke($null, @($op)); $task.Wait(-1) | Out-Null; $task.Result }
[Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType = WindowsRuntime] | Out-Null
[Windows.Graphics.Imaging.BitmapDecoder, Windows.Foundation, ContentType = WindowsRuntime] | Out-Null
[Windows.Storage.StorageFile, Windows.Storage, ContentType = WindowsRuntime] | Out-Null
$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
[Console]::Out.WriteLine('READY')
while ($true) {
  $path = [Console]::In.ReadLine()
  if ($path -eq $null) { break }
  try {
    $f = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($path)) ([Windows.Storage.StorageFile])
    $s = Await ($f.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
    $d = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($s)) ([Windows.Graphics.Imaging.BitmapDecoder])
    $b = Await ($d.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
    $r = Await ($engine.RecognizeAsync($b)) ([Windows.Media.Ocr.OcrResult])
    $lines = New-Object System.Collections.ArrayList
    foreach ($l in $r.Lines) {
      $ws = New-Object System.Collections.ArrayList
      foreach ($w in $l.Words) { [void]$ws.Add(@($w.Text, [int]$w.BoundingRect.X, [int]$w.BoundingRect.Y, [int]$w.BoundingRect.Width, [int]$w.BoundingRect.Height)) }
      [void]$lines.Add($ws)
    }
    $s.Dispose(); $b.Dispose()
    [Console]::Out.WriteLine((ConvertTo-Json -Compress -Depth 5 @{ ok = $true; lines = $lines }))
  } catch {
    [Console]::Out.WriteLine((ConvertTo-Json -Compress @{ ok = $false; error = $_.Exception.Message }))
  }
}
`

type word struct {
	Text       string
	X, Y, W, H int
}

type line struct {
	Text       string `json:"text"`
	X, Y, W, H int    `json:"-"`
	Words      []word `json:"-"`
}

type ocrWorker struct {
	mu  sync.Mutex
	dir string
	seq int
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func newOCR() (*ocrWorker, error) {
	dir, err := os.MkdirTemp("", "screenlink-")
	if err != nil {
		return nil, err
	}
	o := &ocrWorker{dir: dir}
	return o, o.start()
}

func (o *ocrWorker) start() error {
	script := filepath.Join(o.dir, "ocr.ps1")
	if err := os.WriteFile(script, []byte(ocrScript), 0o600); err != nil {
		return err
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	outp, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start OCR worker: %w", err)
	}
	o.cmd, o.in, o.out = cmd, in, bufio.NewReaderSize(outp, 1<<20)
	l, err := o.out.ReadString('\n')
	if err != nil || strings.TrimSpace(l) != "READY" {
		cmd.Process.Kill()
		return fmt.Errorf("OCR worker did not start (%q): Windows OCR needs Windows 10/11 with an OCR language installed", strings.TrimSpace(l))
	}
	return nil
}

func (o *ocrWorker) close() {
	if o.cmd != nil && o.cmd.Process != nil {
		o.in.Close()
		o.cmd.Process.Kill()
	}
	os.RemoveAll(o.dir)
}

// recognize OCRs img, upscaled by scale (2 helps small terminal fonts), and
// returns lines with boxes in img's own coordinates.
func (o *ocrWorker) recognize(img *image.RGBA, scale int) ([]line, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if scale < 1 {
		scale = 1
	}
	b := img.Bounds()
	if b.Dx()*scale > 9900 || b.Dy()*scale > 9900 {
		scale = 1
	}
	// A new file each time: Windows keeps the last one mapped for a moment.
	o.seq++
	path := filepath.Join(o.dir, fmt.Sprintf("frame-%d.bmp", o.seq))
	if old := filepath.Join(o.dir, fmt.Sprintf("frame-%d.bmp", o.seq-3)); o.seq > 3 {
		_ = os.Remove(old)
	}
	if err := writeBMP(path, img, scale); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(o.in, path+"\n"); err != nil {
		o.restart()
		return nil, errors.New("OCR worker stopped; restarted it, try again")
	}
	resp, err := o.out.ReadString('\n')
	if err != nil {
		o.restart()
		return nil, errors.New("OCR worker stopped; restarted it, try again")
	}
	var r struct {
		OK    bool            `json:"ok"`
		Error string          `json:"error"`
		Raw   json.RawMessage `json:"lines"`
	}
	if err := json.Unmarshal([]byte(resp), &r); err != nil {
		return nil, fmt.Errorf("OCR reply: %w", err)
	}
	if !r.OK {
		return nil, errors.New("OCR: " + r.Error)
	}
	var raw [][][]any
	if len(r.Raw) > 0 && string(r.Raw) != "null" {
		if err := json.Unmarshal(r.Raw, &raw); err != nil {
			return nil, fmt.Errorf("OCR lines: %w", err)
		}
	}
	var words []word
	for _, ws := range raw {
		for _, w := range ws {
			if len(w) < 5 {
				continue
			}
			t, _ := w[0].(string)
			n := func(v any) int { f, _ := v.(float64); return int(f) / scale }
			words = append(words, word{Text: t, X: n(w[1]), Y: n(w[2]), W: n(w[3]), H: n(w[4])})
		}
	}
	return rows(words), nil
}

// rows rebuilds screen rows from OCR words. The engine groups text into
// blocks, so a table comes back column by column; here every word whose
// vertical centre lies within the row's band joins that row, left to right.
func rows(words []word) []line {
	sort.Slice(words, func(i, j int) bool { return words[i].Y+words[i].H/2 < words[j].Y+words[j].H/2 })
	var out []line
	for _, w := range words {
		c := w.Y + w.H/2
		if n := len(out); n > 0 {
			l := &out[n-1]
			if lc := l.Y + l.H/2; abs(c-lc)*10 <= max(min(l.H, w.H), 4)*6 {
				l.Words = append(l.Words, w)
				l.Y, l.H = min(l.Y, w.Y), max(l.Y+l.H, w.Y+w.H)-min(l.Y, w.Y)
				continue
			}
		}
		out = append(out, line{Y: w.Y, H: w.H, Words: []word{w}})
	}
	for i := range out {
		l := &out[i]
		sort.Slice(l.Words, func(a, b int) bool { return l.Words[a].X < l.Words[b].X })
		// OCR often splits one word in two ("s uperpower"): join words
		// whose gap is well under one character width; a wide gap is a column.
		var b strings.Builder
		minX, maxX := 1<<30, 0
		for j, w := range l.Words {
			if j > 0 {
				p := l.Words[j-1]
				chars := max(utf8.RuneCountInString(p.Text)+utf8.RuneCountInString(w.Text), 1)
				charW := float64(p.W+w.W) / float64(chars)
				switch gap := float64(w.X - (p.X + p.W)); {
				case gap > charW*3:
					b.WriteString("   ")
				case gap > charW*0.45:
					b.WriteByte(' ')
				}
			}
			b.WriteString(w.Text)
			minX, maxX = min(minX, w.X), max(maxX, w.X+w.W)
		}
		l.Text = b.String()
		l.X, l.W = minX, maxX-minX
	}
	// Drop rows with no letters or digits (dot leaders such as ". . . . :").
	kept := out[:0]
	for _, l := range out {
		if strings.IndexFunc(l.Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			kept = append(kept, l)
		}
	}
	return kept
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (o *ocrWorker) restart() {
	if o.cmd != nil && o.cmd.Process != nil {
		o.cmd.Process.Kill()
	}
	_ = o.start()
}

// writeBMP writes a 24-bit bottom-up BMP for OCR: grayscale, upscaled
// smoothly (bilinear) by an integer factor, and inverted when the picture is
// dark (light text on a dark terminal), since the OCR engine reads dark text
// on a light background best.
func writeBMP(path string, img *image.RGBA, scale int) error {
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	g := make([]float32, sw*sh)
	var sum float64
	for y := 0; y < sh; y++ {
		for x := 0; x < sw; x++ {
			i := img.PixOffset(b.Min.X+x, b.Min.Y+y)
			v := (float32(img.Pix[i])*0.299 + float32(img.Pix[i+1])*0.587 + float32(img.Pix[i+2])*0.114)
			g[y*sw+x] = v
			sum += float64(v)
		}
	}
	if sum/float64(len(g)) < 110 {
		for i := range g {
			g[i] = 255 - g[i]
		}
	}
	w, h := sw*scale, sh*scale
	stride := (w*3 + 3) &^ 3
	data := make([]byte, 54+stride*h)
	copy(data, "BM")
	le := binary.LittleEndian
	le.PutUint32(data[2:], uint32(len(data)))
	le.PutUint32(data[10:], 54)
	le.PutUint32(data[14:], 40)
	le.PutUint32(data[18:], uint32(w))
	le.PutUint32(data[22:], uint32(h))
	le.PutUint16(data[26:], 1)
	le.PutUint16(data[28:], 24)
	le.PutUint32(data[34:], uint32(stride*h))
	inv := 1 / float32(scale)
	for y := 0; y < h; y++ {
		fy := (float32(y)+0.5)*inv - 0.5
		y0 := int(fy)
		if fy < 0 {
			fy, y0 = 0, 0
		}
		y1 := min(y0+1, sh-1)
		ty := fy - float32(y0)
		row := data[54+(h-1-y)*stride:]
		for x := 0; x < w; x++ {
			fx := (float32(x)+0.5)*inv - 0.5
			x0 := int(fx)
			if fx < 0 {
				fx, x0 = 0, 0
			}
			x1 := min(x0+1, sw-1)
			tx := fx - float32(x0)
			top := g[y0*sw+x0]*(1-tx) + g[y0*sw+x1]*tx
			bot := g[y1*sw+x0]*(1-tx) + g[y1*sw+x1]*tx
			v := byte(top*(1-ty) + bot*ty)
			row[x*3], row[x*3+1], row[x*3+2] = v, v, v
		}
	}
	return os.WriteFile(path, data, 0o600)
}
