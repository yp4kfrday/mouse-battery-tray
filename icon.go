package main

import (
	"fmt"
	"syscall"
	"unsafe"

	"mouse-battery-tray/drivers"
)

var (
	modGdi32  = syscall.NewLazyDLL("gdi32.dll")
	modUser32 = syscall.NewLazyDLL("user32.dll")

	procCreateBitmap       = modGdi32.NewProc("CreateBitmap")
	procDeleteObject       = modGdi32.NewProc("DeleteObject")
	procCreateIconIndirect = modUser32.NewProc("CreateIconIndirect")
	procDestroyIcon        = modUser32.NewProc("DestroyIcon")
)

type ICONINFO struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  syscall.Handle
	HbmColor syscall.Handle
}

type RGBA struct {
	R, G, B, A byte
}

var (
	colTransparent = RGBA{0, 0, 0, 0}
	colShadow      = RGBA{10, 15, 26, 210}    // Dark shadow for taskbar contrast
	colBorder      = RGBA{248, 250, 252, 255} // #F8FAFC Crisp white outline
	colBorderSleep = RGBA{148, 163, 184, 255} // #94A3B8 Slate-400 dim outline
	colGlassEmpty  = RGBA{30, 41, 59, 140}    // Translucent dark glass chamber

	// Color states
	colMintGreen = RGBA{52, 211, 153, 255} // #34D399 (100% - 31%)
	colAmber     = RGBA{251, 191, 36, 255}  // #FBBF24 (30% - 16%)
	colRed       = RGBA{239, 68, 68, 255}   // #EF4444 (<= 15%)
	colCharging  = RGBA{34, 197, 94, 255}   // #22C55E Bright Green
	colBolt      = RGBA{254, 240, 138, 255} // #FEF08A Bright Lightning Bolt
	colWheel     = RGBA{226, 232, 240, 255} // #E2E8F0 Wheel rubber
	colWheelRim  = RGBA{71, 85, 105, 255}   // #475569 Wheel tread
)

const (
	iconWidth  = 32
	iconHeight = 32
)

// Canvas holds 32x32 BGRA pixels for Windows HICON.
type Canvas struct {
	pixels [iconWidth * iconHeight * 4]byte
}

func NewCanvas() *Canvas {
	return &Canvas{}
}

func (c *Canvas) setPixel(x, y int, col RGBA) {
	if x < 0 || x >= iconWidth || y < 0 || y >= iconHeight || col.A == 0 {
		return
	}
	idx := (y*iconWidth + x) * 4
	// In BGRA format used by Windows bitmaps
	if col.A == 255 || c.pixels[idx+3] == 0 {
		c.pixels[idx+0] = col.B
		c.pixels[idx+1] = col.G
		c.pixels[idx+2] = col.R
		c.pixels[idx+3] = col.A
	} else {
		srcA := float32(col.A) / 255.0
		dstA := float32(c.pixels[idx+3]) / 255.0
		outA := srcA + dstA*(1.0-srcA)
		if outA > 0 {
			outB := (float32(col.B)*srcA + float32(c.pixels[idx+0])*dstA*(1.0-srcA)) / outA
			outG := (float32(col.G)*srcA + float32(c.pixels[idx+1])*dstA*(1.0-srcA)) / outA
			outR := (float32(col.R)*srcA + float32(c.pixels[idx+2])*dstA*(1.0-srcA)) / outA
			c.pixels[idx+0] = byte(outB)
			c.pixels[idx+1] = byte(outG)
			c.pixels[idx+2] = byte(outR)
			c.pixels[idx+3] = byte(outA * 255.0)
		}
	}
}

// mouseGrid: 0=outside, 1=outer border, 2=interior cavity, 3=wheel, 4=wheel rim
var mouseGrid [32][32]byte

func init() {
	// Ergonomic vertical mouse silhouette
	for y := 2; y <= 28; y++ {
		var xmin, xmax int
		switch {
		case y == 2:
			xmin, xmax = 10, 21
		case y == 3 || y == 4:
			xmin, xmax = 8, 23
		case y >= 5 && y <= 13:
			xmin, xmax = 7, 24
		case y >= 14 && y <= 17:
			// Ergonomic waist taper
			xmin, xmax = 8, 23
		case y >= 18 && y <= 24:
			// Palm flare
			xmin, xmax = 7, 24
		case y == 25:
			xmin, xmax = 8, 23
		case y == 26:
			xmin, xmax = 9, 22
		case y == 27:
			xmin, xmax = 11, 20
		case y == 28:
			xmin, xmax = 13, 18
		default:
			continue
		}

		for x := xmin; x <= xmax; x++ {
			if x == xmin || x == xmax || y == 2 || y == 28 {
				mouseGrid[y][x] = 1 // Border
			} else {
				mouseGrid[y][x] = 2 // Interior cavity
			}
		}
	}

	// Scroll wheel slot and body (X: 14..17, Y: 3..8)
	for y := 3; y <= 8; y++ {
		mouseGrid[y][14] = 4 // Rim
		mouseGrid[y][15] = 3 // Wheel
		mouseGrid[y][16] = 3 // Wheel
		mouseGrid[y][17] = 4 // Rim
	}
	// Scroll wheel caps and divider seam
	mouseGrid[2][15] = 1
	mouseGrid[2][16] = 1
	mouseGrid[9][14] = 1
	mouseGrid[9][15] = 1
	mouseGrid[9][16] = 1
	mouseGrid[9][17] = 1

	// Button seam line down from scroll wheel
	mouseGrid[10][15] = 1
	mouseGrid[10][16] = 1
	mouseGrid[11][15] = 1
	mouseGrid[11][16] = 1
}

// 7x9 crisp lightning bolt glyph for charging
var glyphBolt7x9 = []uint8{
	0b0000110,
	0b0001100,
	0b0011000,
	0b0111111,
	0b0000110,
	0b0001100,
	0b0011000,
	0b0110000,
	0b1100000,
}

// 5x5 crisp cross glyph for disconnected
var glyphCross5x5 = []uint8{
	0b10001,
	0b01010,
	0b00100,
	0b01010,
	0b10001,
}

// 10x6 crisp "--" dashed glyph for sleeping
var glyphDash10x6 = []uint16{
	0b0000000000,
	0b0000000000,
	0b1111001111,
	0b1111001111,
	0b0000000000,
	0b0000000000,
}

func (c *Canvas) drawGlyph(x0, y0 int, glyph []uint8, width int, col RGBA, shadow bool) {
	for row, bits := range glyph {
		for colIdx := 0; colIdx < width; colIdx++ {
			if (bits & (1 << (width - 1 - colIdx))) != 0 {
				px := x0 + colIdx
				py := y0 + row
				if shadow {
					c.setPixel(px+1, py+1, colShadow)
				}
				c.setPixel(px, py, col)
			}
		}
	}
}

func (c *Canvas) drawGlyph16(x0, y0 int, glyph []uint16, width int, col RGBA, shadow bool) {
	for row, bits := range glyph {
		for colIdx := 0; colIdx < width; colIdx++ {
			if (bits & (1 << (width - 1 - colIdx))) != 0 {
				px := x0 + colIdx
				py := y0 + row
				if shadow {
					c.setPixel(px+1, py+1, colShadow)
				}
				c.setPixel(px, py, col)
			}
		}
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RenderBatteryIcon generates a crisp 32x32 Liquid Mouse HICON reflecting real hardware battery state.
func RenderBatteryIcon(state drivers.BatteryState) (syscall.Handle, error) {
	c := NewCanvas()

	borderCol := colBorder
	if state.Status == drivers.StatusAsleep || state.Status == drivers.StatusDisconnected {
		borderCol = colBorderSleep
	}

	// 1. Draw 1px drop shadow around outer perimeter only (ensures contrast on light taskbars)
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			if mouseGrid[y][x] == 0 {
				hasBodyNeighbor := (y > 0 && mouseGrid[y-1][x] > 0) ||
					(x > 0 && mouseGrid[y][x-1] > 0) ||
					(y > 0 && x > 0 && mouseGrid[y-1][x-1] > 0)
				if hasBodyNeighbor {
					c.setPixel(x, y, colShadow)
				}
			}
		}
	}

	// 2. Base mouse container rendering
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			switch mouseGrid[y][x] {
			case 1: // Outer border
				c.setPixel(x, y, borderCol)
			case 2: // Empty translucent glass cavity
				c.setPixel(x, y, colGlassEmpty)
			case 3: // Wheel center rubber
				c.setPixel(x, y, colWheel)
			case 4: // Wheel rim / tread
				c.setPixel(x, y, colWheelRim)
			}
		}
	}

	// 3. Liquid Fill based on state and percentage
	switch state.Status {
	case drivers.StatusCharging:
		// Charging: Bright green fill + lightning bolt
		pct := state.Percent
		if pct < 1 {
			pct = 1
		} else if pct > 100 {
			pct = 100
		}

		fillRows := int((float32(pct) / 100.0) * 22.0)
		if fillRows < 1 {
			fillRows = 1
		}
		surfaceY := 28 - fillRows

		meniscusCol := RGBA{
			R: byte(minInt(255, int(colCharging.R)+45)),
			G: byte(minInt(255, int(colCharging.G)+45)),
			B: byte(minInt(255, int(colCharging.B)+45)),
			A: 255,
		}

		for y := surfaceY; y <= 27; y++ {
			for x := 0; x < 32; x++ {
				if mouseGrid[y][x] == 2 {
					if y == surfaceY {
						c.setPixel(x, y, meniscusCol)
					} else {
						c.setPixel(x, y, colCharging)
					}
				}
			}
		}

		// Centered lightning bolt
		c.drawGlyph(13, 14, glyphBolt7x9, 7, colBolt, true)

	case drivers.StatusDischarging:
		// Discharging: Liquid filled proportionally from bottom to top
		// 100% – 31%: Mint green (#34D399)
		// 30% – 16%:  Amber/yellow (#FBBF24)
		// <= 15%:     Red (#EF4444)
		var liquidCol RGBA
		if state.Percent > 30 {
			liquidCol = colMintGreen
		} else if state.Percent >= 16 {
			liquidCol = colAmber
		} else {
			liquidCol = colRed
		}

		pct := state.Percent
		if pct < 1 {
			pct = 1
		} else if pct > 100 {
			pct = 100
		}

		fillRows := int((float32(pct) / 100.0) * 22.0)
		if fillRows < 1 {
			fillRows = 1
		}
		surfaceY := 28 - fillRows

		meniscusCol := RGBA{
			R: byte(minInt(255, int(liquidCol.R)+45)),
			G: byte(minInt(255, int(liquidCol.G)+45)),
			B: byte(minInt(255, int(liquidCol.B)+45)),
			A: 255,
		}

		for y := surfaceY; y <= 27; y++ {
			for x := 0; x < 32; x++ {
				if mouseGrid[y][x] == 2 {
					if y == surfaceY {
						c.setPixel(x, y, meniscusCol)
					} else {
						c.setPixel(x, y, liquidCol)
					}
				}
			}
		}

	case drivers.StatusAsleep:
		// Sleeping: Dim gray outline with crisp "--" centered
		c.drawGlyph16(11, 17, glyphDash10x6, 10, colBorderSleep, true)

	case drivers.StatusDisconnected:
		fallthrough
	default:
		// Disconnected: Dim gray outline with small red "×" centered
		c.drawGlyph(14, 16, glyphCross5x5, 5, colRed, true)
	}

	// 4. Create native Windows HICON
	hbmColor, _, err := procCreateBitmap.Call(
		uintptr(iconWidth),
		uintptr(iconHeight),
		1,
		32,
		uintptr(unsafe.Pointer(&c.pixels[0])),
	)
	if hbmColor == 0 {
		return 0, fmt.Errorf("CreateBitmap color failed: %w", err)
	}
	defer procDeleteObject.Call(hbmColor)

	maskBits := make([]byte, (iconWidth*iconHeight)/8)
	hbmMask, _, err := procCreateBitmap.Call(
		uintptr(iconWidth),
		uintptr(iconHeight),
		1,
		1,
		uintptr(unsafe.Pointer(&maskBits[0])),
	)
	if hbmMask == 0 {
		return 0, fmt.Errorf("CreateBitmap mask failed: %w", err)
	}
	defer procDeleteObject.Call(hbmMask)

	ii := ICONINFO{
		FIcon:    1, // Icon (not cursor)
		XHotspot: 0,
		YHotspot: 0,
		HbmMask:  syscall.Handle(hbmMask),
		HbmColor: syscall.Handle(hbmColor),
	}

	hIcon, _, err := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	if hIcon == 0 {
		return 0, fmt.Errorf("CreateIconIndirect failed: %w", err)
	}

	return syscall.Handle(hIcon), nil
}

// DestroyIconHandle releases an HICON allocated by RenderBatteryIcon.
func DestroyIconHandle(hIcon syscall.Handle) {
	if hIcon != 0 {
		procDestroyIcon.Call(uintptr(hIcon))
	}
}
