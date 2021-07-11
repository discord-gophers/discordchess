package discordchess

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"strings"
	"unicode"

	_ "embed"

	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

//go:embed assets/font.ttf
var fontData []byte

type DrawerPiece int

const (
	PieceWhite = DrawerPiece(iota)
	PieceBlack
)

type DrawerMark struct {
	color  color.Color
	sx, sy int
}

type Drawer struct {
	squareBlack color.Color
	squareWhite color.Color
	pieceBlack  color.Color
	pieceWhite  color.Color
	pad         int

	piecesFace font.Face
	textFace   font.Face
}

func (d *Drawer) Close() {
	d.piecesFace.Close()
	d.textFace.Close()
}

func NewDrawer(opts ...func(d *Drawer)) (*Drawer, error) {
	sff, err := sfnt.Parse(fontData)
	if err != nil {
		return nil, err
	}
	piecesFace, err := opentype.NewFace(sff, &opentype.FaceOptions{
		Size:    75,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	textFace, err := opentype.NewFace(sff, &opentype.FaceOptions{
		Size:    18,
		DPI:     72,
		Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}

	d := &Drawer{
		squareBlack: color.RGBA{100, 100, 120, 255},
		squareWhite: color.RGBA{200, 200, 200, 255},
		pieceBlack:  color.Black,
		pieceWhite:  color.White,
		pad:         2 * 8,
		piecesFace:  piecesFace,
		textFace:    textFace,
	}
	for _, fn := range opts {
		fn(d)
	}
	return d, nil
}

var piecesMap = map[rune]rune{
	'r': '♜',
	'n': '♞',
	'b': '♝',
	'k': '♚',
	'q': '♛',
	'p': '♟',
}

func (d *Drawer) Draw(im draw.Image, fen string, marks ...DrawerMark) error {
	// Fill with some color
	draw.Src.Draw(
		im,
		im.Bounds(),
		image.NewUniform(mulColor(d.squareWhite, .9)),
		image.Point{},
	)

	r := im.Bounds()

	// Draw checker pattern
	s := (r.Dx() - d.pad) / 8
	for sy := 0; sy < 8; sy++ {
		for sx := 0; sx < 8; sx++ {
			x, y := d.pad+sx*s, sy*s
			draw.Src.Draw(
				im,
				image.Rect(x, y, x+s, y+s),
				image.NewUniform(d.squareColor(sx, sy)),
				image.Point{},
			)

		}
	}
	// Draw marks
	for _, m := range marks {
		x, y := d.pad+m.sx*s, m.sy*s
		draw.DrawMask(
			im,
			image.Rect(x, y, x+s, y+s),
			image.NewUniform(m.color),
			image.Pt(0, 0),
			image.NewUniform(m.color),
			image.Pt(0, 0),
			draw.Over,
		)
	}

	// Parse notation and draw pieces
	if err := d.drawFen(im, fen); err != nil {
		return err
	}

	// Draw rulers
	for i := 0; i < 8; i++ {
		d.drawText(
			im,
			d.pad/8,
			s/2+s*i,
			color.Black,
			fmt.Sprintf("%d", 8-i),
		)
		d.drawText(
			im,
			d.pad+s/2+s*i,
			r.Dy()-d.textFace.Metrics().Descent.Ceil(),
			color.Black,
			fmt.Sprintf("%c", 'a'+i),
		)
	}
	return nil
}

func (d *Drawer) squareColor(sx, sy int) color.Color {
	if sx&1^sy&1 == 1 {
		return d.squareBlack
	}
	return d.squareWhite
}

func (d *Drawer) drawFen(im draw.Image, fen string) error {
	rows := strings.Split(fen, "/")
	for sy, r := range rows {
		sx := 0
		for _, s := range r {
			if s == ' ' {
				return nil
			}
			pc := PieceWhite
			var p rune
			if unicode.IsLower(s) {
				pc = PieceBlack
			}
			p, ok := piecesMap[unicode.ToLower(s)]
			if !ok {
				e := int(s - '0')
				if e < 0 || e > 8 {
					return errors.New("error parsing fen")
				}
				sx += e
				continue
			}
			d.drawPiece(im, sx, sy, pc, p)
			sx++
		}
	}
	return nil
}

func (d Drawer) drawText(im draw.Image, x, y int, c color.Color, s string) {
	fd := font.Drawer{
		Dst:  im,
		Face: d.textFace,
		Src:  image.NewUniform(c),
		Dot:  fixed.P(x, y),
	}
	fd.DrawString(s)
}

func (d Drawer) drawPiece(im draw.Image, sx, sy int, p DrawerPiece, r rune) {
	s := (im.Bounds().Dx() - d.pad) / 8

	// bnd, _, _ := d.piecesFace.GlyphBounds(r)
	// n := (bnd.Max.X).Floor()
	//
	// Tried with different fonts and no matter if I used font metrics it
	// would get wrong, at least this values look right with the current
	// font 'GNU FreeSerif' and 512x512 bounds
	//
	fontFixX, fontFixY := 3, -9

	ssx := fontFixX + d.pad + sx*s
	ssy := fontFixY + s + sy*s

	fd := font.Drawer{
		Dst:  im,
		Face: d.piecesFace,
	}
	c := d.pieceWhite
	border := d.pieceBlack
	if p == PieceBlack {
		c, border = border, mulColor(c, 0.7)
	}

	sr := string(r)
	// Hackery border
	{
		fd.Src = image.NewUniform(border)
		fd.Dot = fixed.P(ssx-1, ssy-1)
		fd.DrawString(sr)
		fd.Dot = fixed.P(ssx+1, ssy+1)
		fd.DrawString(sr)
	}

	fd.Src = image.NewUniform(c)
	fd.Dot = fixed.P(ssx, ssy)
	fd.DrawString(sr)
}

func mulColor(c color.Color, factor float64) color.Color {
	r, g, b, a := c.RGBA()
	res := color.RGBA{
		byte(float64(r) / math.MaxUint16 * factor * 255),
		byte(float64(g) / math.MaxUint16 * factor * 255),
		byte(float64(b) / math.MaxUint16 * factor * 255),
		byte(float64(a) / math.MaxUint16 * 255),
	}
	return res
}

func WithSquareColors(w, b color.Color) func(d *Drawer) {
	return func(d *Drawer) {
		d.squareWhite = w
		d.squareBlack = b
	}
}

func WithPieceColors(w, b color.Color) func(d *Drawer) {
	return func(d *Drawer) {
		d.pieceWhite = w
		d.pieceBlack = b
	}
}
