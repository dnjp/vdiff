package main

import (
	"bufio"
	"flag"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"9fans.net/go/draw"
	"9fans.net/go/plumb"
)

const (
	lFile = iota
	lSep
	lAdd
	lDel
	lNone
	nCols
)

const (
	scrollWidth = 12
	scrollGap   = 2
	margin      = 8
	hPadding    = 4
	vPadding    = 2
	ellipsis    = "..."
)

type line struct {
	t int    // line type (lFile, lSep, lAdd, lDel, lNone)
	n int    // source line number
	s string // raw text
}

type block struct {
	b     *draw.Image
	r     image.Rectangle // local coords (origin 0,0)
	sr    image.Rectangle // screen coords
	v     bool            // expanded
	f     string          // file name; empty if unset
	lines []*line
}

type col struct {
	bg *draw.Image
	fg *draw.Image
}

var (
	disp       *draw.Display
	mc         *draw.Mousectl
	kc         *draw.Keyboardctl
	sr         image.Rectangle
	scrollR    image.Rectangle
	scrPosR    image.Rectangle
	viewR      image.Rectangle
	cols       [nCols]col
	scrlCol    col
	bord       *draw.Image
	expander   [2]*draw.Image
	totalH     int
	viewH      int
	scrollSize int
	offset     int
	lineH      int
	scrolling  bool
	oldButtons int
	blocks     []*block
	maxLength  int
	dpan       int
	ellipsisW  int
	spaceW     int
)

// plumb opens file:line in the editor via the plumber.
func plumbLine(f string, l int) {
	go func() {
		wd, err := os.Getwd()
		if err != nil {
			wd = "."
		}
		msg := &plumb.Message{
			Src:  "vgit",
			Dst:  "edit",
			Dir:  wd,
			Type: "text",
			Data: []byte(fmt.Sprintf("%s:%d", f, l)),
		}
		fid, err := plumb.Open("send", 1) // plan9.OWRITE
		if err != nil {
			fmt.Fprintf(os.Stderr, "plumb open: %v\n", err)
			return
		}
		defer fid.Close()
		if err := msg.Send(fid); err != nil {
			fmt.Fprintf(os.Stderr, "plumb send: %v\n", err)
		}
	}()
}

func ecolor(n draw.Color) *draw.Image {
	img, err := disp.AllocImage(image.Rect(0, 0, 1, 1), disp.ScreenImage.Pix, true, n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allocimage: %v\n", err)
		os.Exit(1)
	}
	return img
}

func initCol(c *col, fg, bg draw.Color) {
	c.fg = ecolor(fg)
	c.bg = ecolor(bg)
}

func initCols(black bool) {
	if black {
		// Dark scheme: RGB channels are inverted relative to the light scheme.
		// Original C: 0x888888FF^(~0xFF) = 0x888888FF^0xFFFFFF00 = 0x777777FF
		bord = ecolor(0x777777FF)
		initCol(&scrlCol, draw.Black, 0x666666FF)
		initCol(&cols[lFile], draw.White, 0x333333FF)
		initCol(&cols[lSep], draw.Black, draw.PurpleBlue)
		initCol(&cols[lAdd], draw.White, 0x002800FF)
		initCol(&cols[lDel], draw.White, 0x3F0000FF)
		initCol(&cols[lNone], draw.White, draw.Black)
	} else {
		bord = ecolor(0x888888FF)
		initCol(&scrlCol, draw.White, 0x999999FF)
		initCol(&cols[lFile], draw.Black, 0xEFEFEFFF)
		initCol(&cols[lSep], draw.Black, 0xEAFFFFFF)
		initCol(&cols[lAdd], draw.Black, 0xE6FFEDFF)
		initCol(&cols[lDel], draw.Black, 0xFFEEF0FF)
		initCol(&cols[lNone], draw.Black, draw.White)
	}
}

// initIcons builds the two triangle expander icons (collapsed=right, expanded=down).
func initIcons() {
	w := disp.Font.Height
	h := disp.Font.Height

	var err error

	// Collapsed: right-pointing triangle.
	expander[0], err = disp.AllocImage(image.Rect(0, 0, w, h), disp.ScreenImage.Pix, false, draw.NoFill)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allocimage: %v\n", err)
		os.Exit(1)
	}
	expander[0].Draw(expander[0].R, cols[lFile].bg, nil, draw.ZP)
	expander[0].FillPoly([]image.Point{
		draw.Pt(w/4, h/4),
		draw.Pt(w/4, 3*h/4),
		draw.Pt(3*w/4, h/2),
		draw.Pt(w/4, h/4),
	}, 0, bord, draw.ZP)

	// Expanded: down-pointing triangle.
	expander[1], err = disp.AllocImage(image.Rect(0, 0, w, h), disp.ScreenImage.Pix, false, draw.NoFill)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allocimage: %v\n", err)
		os.Exit(1)
	}
	expander[1].Draw(expander[1].R, cols[lFile].bg, nil, draw.ZP)
	expander[1].FillPoly([]image.Point{
		draw.Pt(w/4, h/4),
		draw.Pt(3*w/4, h/4),
		draw.Pt(w/2, 3*h/4),
		draw.Pt(w/4, h/4),
	}, 0, bord, draw.ZP)

	disp.Flush() //nolint:errcheck
}

// renderLine draws a single diff line onto img at rectangle r.
// pad is extra left indent (for blocks that have a filename header).
// lt selects the color scheme; ls is the raw text.
// Horizontal panning (dpan) and tab expansion (4-stop) are handled here.
func renderLine(img *draw.Image, r image.Rectangle, pad, lt int, ls string) {
	img.Draw(r, cols[lt].bg, nil, draw.ZP)
	p := draw.Pt(r.Min.X+pad+hPadding, r.Min.Y+(r.Dy()-disp.Font.Height)/2)
	skip := dpan / spaceW // characters to skip for horizontal panning
	col := 0              // output column for tab-stop tracking

	for _, rn := range ls {
		if rn == '\t' {
			spaces := 4 - col%4
			for i := 0; i < spaces; i++ {
				if skip > 0 {
					skip--
					col++
					continue
				}
				p = img.Runes(p, cols[lt].bg, draw.ZP, disp.Font, []rune{'█'})
				col++
			}
		} else {
			if skip > 0 {
				skip--
				col++
				continue
			}
			if p.X+hPadding+spaceW+ellipsisW >= img.R.Max.X {
				img.String(p, cols[lt].fg, draw.ZP, disp.Font, ellipsis)
				break
			}
			p = img.Runes(p, cols[lt].fg, draw.ZP, disp.Font, []rune{rn})
			col++
		}
	}
}

func renderBlock(b *block) {
	r := b.r.Inset(1)
	b.b.Draw(b.r, cols[lNone].bg, nil, draw.ZP)
	pad := 0
	if b.f != "" {
		pad = margin
		lr := r
		lr.Max.Y = r.Min.Y + lineH
		br := expander[0].R.Add(draw.Pt(lr.Min.X+hPadding, lr.Min.Y+vPadding))
		b.b.Border(b.r, 1, bord, draw.ZP)
		renderLine(b.b, lr, expander[0].R.Dx()+hPadding, lFile, b.f)
		vi := 0
		if b.v {
			vi = 1
		}
		b.b.Draw(br, expander[vi], nil, draw.ZP)
		r.Min.Y += lineH
	}
	if !b.v {
		return
	}
	for i, l := range b.lines {
		lr := image.Rect(r.Min.X, r.Min.Y+i*lineH, r.Max.X, r.Min.Y+(i+1)*lineH)
		renderLine(b.b, lr, pad, l.t, l.s)
	}
}

func redraw() {
	screen := disp.ScreenImage
	screen.Draw(sr, cols[lNone].bg, nil, draw.ZP)
	screen.Draw(scrollR, scrlCol.bg, nil, draw.ZP)

	if viewH < totalH {
		h := int(float64(viewH) / float64(totalH) * float64(scrollR.Dy()))
		y := int(float64(offset) / float64(totalH) * float64(scrollR.Dy()))
		ye := scrollR.Min.Y + y + h - 1
		if ye >= scrollR.Max.Y {
			ye = scrollR.Max.Y - 1
		}
		scrPosR = image.Rect(scrollR.Min.X, scrollR.Min.Y+y+1, scrollR.Max.X-1, ye)
	} else {
		scrPosR = image.Rect(scrollR.Min.X, scrollR.Min.Y, scrollR.Max.X-1, scrollR.Max.Y)
	}
	screen.Draw(scrPosR, scrlCol.fg, nil, draw.ZP)

	vmin := viewR.Min.Y + offset
	vmax := viewR.Max.Y + offset
	clipr := screen.Clipr
	screen.ReplClipr(false, viewR)
	for _, b := range blocks {
		if b.sr.Min.Y <= vmax && b.sr.Max.Y >= vmin {
			renderBlock(b)
			screen.Draw(b.sr.Add(draw.Pt(0, -offset)), b.b, nil, draw.ZP)
		}
	}
	screen.ReplClipr(false, clipr)
	disp.Flush() //nolint:errcheck
}

func pan(off int) {
	if len(blocks) == 0 {
		return
	}
	max := hPadding + margin + hPadding + maxLength*spaceW + 2*ellipsisW - blocks[0].r.Dx()
	dpan += off * spaceW
	if dpan < 0 || max <= 0 {
		dpan = 0
	} else if dpan > max {
		dpan = max
	}
	redraw()
}

func clampOffset() {
	if offset < 0 {
		offset = 0
	}
	if offset+viewH > totalH {
		offset = totalH - viewH
	}
}

func scroll(off int) {
	if off < 0 && offset <= 0 {
		return
	}
	if off > 0 && offset+viewH > totalH {
		return
	}
	offset += off
	clampOffset()
	redraw()
}

func blockResize(b *block) {
	w := viewR.Dx() - 2
	h := 2
	if b.f != "" {
		h += lineH
	}
	if b.v {
		h += len(b.lines) * lineH
	}
	b.r = image.Rect(0, 0, w, h)
	if b.b != nil {
		b.b.Free() //nolint:errcheck
		b.b = nil
	}
	var err error
	b.b, err = disp.AllocImage(b.r, disp.ScreenImage.Pix, false, draw.NoFill)
	if err != nil {
		fmt.Fprintf(os.Stderr, "allocimage: %v\n", err)
		os.Exit(1)
	}
}

func eResize(isNew bool) {
	if isNew {
		if err := disp.Attach(draw.RefNone); err != nil {
			fmt.Fprintf(os.Stderr, "cannot reattach: %v\n", err)
			os.Exit(1)
		}
	}
	screen := disp.ScreenImage
	sr = screen.R
	scrollR = sr
	scrollR.Max.X = scrollR.Min.X + scrollWidth + scrollGap
	listR := sr
	listR.Min.X = scrollR.Max.X
	viewR = listR.Inset(margin)
	viewH = viewR.Dy()
	lineH = vPadding + disp.Font.Height + vPadding
	totalH = -margin + vPadding + 1
	p := viewR.Min.Add(draw.Pt(0, totalH))
	for _, b := range blocks {
		blockResize(b)
		b.sr = b.r.Add(p)
		p.Y += margin + b.r.Dy()
		totalH += margin + b.r.Dy()
	}
	totalH = totalH - margin + vPadding
	scrollSize = viewH / 2
	if offset > 0 && offset+viewH > totalH {
		offset = totalH - viewH
	}
	redraw()
}

func eKeyboard(k rune) {
	switch k {
	case 'q', draw.KeyDelete:
		os.Exit(0)
	case draw.KeyHome:
		scroll(-totalH)
	case draw.KeyEnd:
		scroll(totalH)
	case draw.KeyPageUp:
		scroll(-viewH)
	case draw.KeyPageDown:
		scroll(viewH)
	case draw.KeyUp:
		scroll(-scrollSize)
	case draw.KeyDown:
		scroll(scrollSize)
	case draw.KeyLeft:
		pan(-4)
	case draw.KeyRight:
		pan(4)
	}
}

func blockMouse(b *block, m draw.Mouse) {
	n := (m.Y + offset - b.sr.Min.Y) / lineH
	if n == 0 && b.f != "" && m.Buttons&1 != 0 {
		b.v = !b.v
		eResize(false)
	} else if n > 0 && m.Buttons&4 != 0 && n-1 < len(b.lines) {
		l := b.lines[n-1]
		if l.t != lSep {
			plumbLine(b.f, l.n)
		}
	}
}

func eMouse(m draw.Mouse) {
	if oldButtons == 0 && m.Buttons != 0 && m.Point.In(scrollR) {
		scrolling = true
	} else if m.Buttons == 0 {
		scrolling = false
	}

	if scrolling {
		n := 5 * (m.Y - scrollR.Min.Y)
		if m.Buttons&1 != 0 {
			scroll(-n)
			oldButtons = m.Buttons
			return
		} else if m.Buttons&2 != 0 {
			if scrollR.Dy() > 0 {
				offset = (m.Y - scrollR.Min.Y) * totalH / scrollR.Dy()
			}
			clampOffset()
			redraw()
		} else if m.Buttons&4 != 0 {
			scroll(n)
			oldButtons = m.Buttons
			return
		}
	} else if m.Buttons&8 != 0 {
		scroll(-scrollSize)
	} else if m.Buttons&16 != 0 {
		scroll(scrollSize)
	} else if m.Buttons != 0 && m.Point.In(viewR) {
		for _, b := range blocks {
			if m.Point.Add(draw.Pt(0, offset)).In(b.sr) {
				blockMouse(b, m)
				break
			}
		}
	}
	oldButtons = m.Buttons
}

func lineType(s string) int {
	switch {
	case strings.HasPrefix(s, "+++"):
		return lFile
	case strings.HasPrefix(s, "---"):
		if len(s) > 4 {
			return lFile
		}
	case strings.HasPrefix(s, "@@"):
		return lSep
	case strings.HasPrefix(s, "+"):
		return lAdd
	case strings.HasPrefix(s, "-"):
		return lDel
	}
	return lNone
}

// lineNo extracts the new-file start line number from a @@ header.
// e.g. "@@ -1,3 +4,7 @@" → 4
func lineNo(s string) int {
	fields := strings.Fields(s)
	if len(fields) <= 2 {
		return -1
	}
	f := fields[2]
	if len(f) > 0 && f[0] == '+' {
		f = f[1:]
	}
	if i := strings.IndexByte(f, ','); i >= 0 {
		f = f[:i]
	}
	n, _ := strconv.Atoi(f)
	return n
}

func parse(r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1<<20), 1<<20) // 1 MiB per line

	b := &block{v: true}
	blocks = append(blocks, b)
	ab := false
	n := 0

	for scanner.Scan() {
		s := scanner.Text()
		t := lineType(s)
		switch t {
		case lFile:
			if s[0] == '-' {
				b = &block{v: true}
				blocks = append(blocks, b)
				if len(s) >= 6 && strings.HasPrefix(s[4:], "a/") {
					ab = true
				}
			} else if len(s) > 4 { // '+'
				f := s[4:]
				if ab && strings.HasPrefix(f, "b/") {
					f = f[1:] // "b/path" → "/path"
					if _, err := os.Stat(f); err != nil {
						f = f[1:] // "/path" → "path"
					}
				}
				if i := strings.IndexByte(f, '\t'); i >= 0 {
					f = f[:i]
				}
				b.f = f
			}
		default:
			if t == lSep {
				n = lineNo(s) - 1
			} else if t == lAdd || t == lNone {
				n++
			}
			b.lines = append(b.lines, &line{t: t, n: n, s: s})
			if len(s) > maxLength {
				maxLength = len(s)
			}
		}
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage: %s [-b] diff [<path>...]\n", os.Args[0])
	os.Exit(2)
}

func startUI(black bool) {
	var err error
	disp, err = draw.Init(nil, "", "vgit", "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "initdraw: %v\n", err)
		os.Exit(1)
	}

	mc = disp.InitMouse()
	kc = disp.InitKeyboard()

	initCols(black)
	initIcons()

	spaceW = disp.Font.StringWidth(" ")
	ellipsisW = disp.Font.StringWidth(ellipsis)

	eResize(false)

	for {
		disp.Flush() //nolint:errcheck
		select {
		case m := <-mc.C:
			mc.Mouse = m
			eMouse(m)
		case <-mc.Resize:
			eResize(true)
		case k := <-kc.C:
			eKeyboard(k)
		}
	}
}

func cmdDiff(black bool, paths []string) {
	args := append([]string{"diff"}, paths...)
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	r, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pipe: %v\n", err)
		os.Exit(1)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "git diff: %v\n", err)
		os.Exit(1)
	}
	parse(r)
	cmd.Wait() //nolint:errcheck

	if len(blocks) == 0 {
		fmt.Fprintln(os.Stderr, "no diff")
		os.Exit(0)
	}
	startUI(black)
}

func main() {
	black := flag.Bool("b", false, "use dark color scheme")
	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		usage()
	}

	switch args[0] {
	case "diff":
		cmdDiff(*black, args[1:])
	default:
		usage()
	}
}
