package main

import (
	"bytes"
	"encoding/binary"
	"hash/adler32"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// The addon's View on website click draws a packet on screen and takes a screenshot, because the client keeps
// LaunchURL for Blizzard UI: "WDB2", session and click numbers, the path's length (2 bytes), the record's public
// path, Adler-32 of everything before it. Rows of 120 cells of 8 UI units, a unit being 1/768 of the screen height,
// centred near the top.
const (
	packetMagic   = "WDB2"
	packetHeader  = 14
	packetColumns = 120
	packetCell    = 8.0
	uiHeight      = 768.0
)

// The client writes JPEG unless the player chose another screenshot format.
var screenshotFormats = map[string]bool{".jpg": true, ".jpeg": true, ".png": true}

type lookupPacket struct {
	session, sequence uint32
	path              string
}

// lookupRequests remembers what the program already read: screenshots of the player's own are read once and kept,
// and each click opens one page however often its file is seen.
type lookupRequests struct {
	since  time.Time
	read   map[string]bool
	opened map[[2]uint32]bool
	unread []string
}

func newLookupRequests() *lookupRequests {
	return &lookupRequests{since: time.Now(), read: map[string]bool{}, opened: map[[2]uint32]bool{}}
}

func screenshotFolders(found []install) []string {
	var dirs []string
	for _, i := range found {
		dirs = append(dirs, filepath.Join(i.root, i.folder, "Screenshots"))
	}
	return dirs
}

// open reads the screenshots taken since the program started; a file the game is still writing stays unread until
// its next change.
func (r *lookupRequests) open(s *state, origin string, dirs []string) {
	r.unread = nil
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || !screenshotFormats[strings.ToLower(filepath.Ext(path))] ||
				r.read[path] || info.ModTime().Before(r.since) {
				continue
			}
			packet, found, err := readPacket(path)
			if err != nil {
				r.unread = append(r.unread, path)
				continue
			}
			r.read[path] = true
			if found {
				r.deliver(s, origin, path, packet)
			}
		}
	}
}

func (r *lookupRequests) deliver(s *state, origin, path string, p lookupPacket) {
	os.Remove(path)
	click := [2]uint32{p.session, p.sequence}
	if r.opened[click] {
		return
	}
	r.opened[click] = true
	address, err := url.Parse(origin + p.path)
	if err == nil {
		err = openBrowser(site(address.String()))
	}
	if err != nil {
		s.log("lookup result=failed path=%s: %v", p.path, err)
		return
	}
	s.log("lookup result=opened path=%s", p.path)
}

func readPacket(path string) (lookupPacket, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return lookupPacket{}, false, err
	}
	picture, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return lookupPacket{}, false, err
	}
	p, found := findPacket(picture)
	return p, found, nil
}

// findPacket looks for the strip's first row from the top, since the game's top inset varies with the window.
func findPacket(picture image.Image) (lookupPacket, bool) {
	bounds := picture.Bounds()
	cell := packetCell * float64(bounds.Dy()) / uiHeight
	left := float64(bounds.Min.X) + (float64(bounds.Dx())-(packetColumns+2)*cell)/2 + cell
	for top := float64(bounds.Min.Y); top+cell <= float64(bounds.Max.Y); top++ {
		if p, ok := readStrip(cells{picture, left, top, cell}); ok {
			return p, true
		}
	}
	return lookupPacket{}, false
}

type cells struct {
	picture         image.Image
	left, top, cell float64
}

// byteAt reads the strip's byte n; a cell below the picture reads dark and fails the checksum.
func (c cells) byteAt(n int) byte {
	var value byte
	for bit := range 8 {
		at := n*8 + bit
		x, y := c.left+(float64(at%packetColumns)+0.5)*c.cell, c.top+(float64(at/packetColumns)+0.5)*c.cell
		value = value<<1 | bright(c.picture.At(int(x), int(y)))
	}
	return value
}

// readStrip reads the magic first, so a row of the player's own picture costs four bytes.
func readStrip(c cells) (lookupPacket, bool) {
	raw := make([]byte, 0, packetHeader)
	for n := range packetHeader {
		raw = append(raw, c.byteAt(n))
		if n < len(packetMagic) && raw[n] != packetMagic[n] {
			return lookupPacket{}, false
		}
	}
	for n := range int(binary.BigEndian.Uint16(raw[12:14])) + 4 {
		raw = append(raw, c.byteAt(packetHeader+n))
	}
	return decodePacket(raw)
}

func bright(c color.Color) byte {
	if color.GrayModel.Convert(c).(color.Gray).Y >= 128 {
		return 1
	}
	return 0
}

func decodePacket(raw []byte) (lookupPacket, bool) {
	body := raw[:len(raw)-4]
	if adler32.Checksum(body) != binary.BigEndian.Uint32(raw[len(body):]) {
		return lookupPacket{}, false
	}
	p := lookupPacket{session: binary.BigEndian.Uint32(raw[4:8]), sequence: binary.BigEndian.Uint32(raw[8:12]),
		path: string(body[packetHeader:])}
	return p, publicPath(p.path)
}

// publicPath admits a path below the site's origin only: never another host, a scheme or a control character.
func publicPath(path string) bool {
	return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//") && utf8.ValidString(path) &&
		!strings.ContainsFunc(path, func(r rune) bool { return unicode.IsControl(r) || r == '\\' })
}
