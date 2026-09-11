package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

// This is the FR-JOURNAL-06 blinding guard. A session withholds the date from every payload for
// eight hundred bars; one screenshot carrying "DateTimeOriginal: 2023:03:14 08:31:00" undoes it,
// and the leak arrives from inside the trader's own upload.

func swatch(width, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	return img
}

func encodeJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, nil); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out.Bytes()
}

// withEXIF splices a real APP1/Exif segment into a JPEG, right after the SOI marker, the way a
// camera or a screenshot tool does. The payload carries a capture date and a GPS tag — the two
// things that would identify when and where, and therefore what.
func withEXIF(t *testing.T, raw []byte) []byte {
	t.Helper()
	if len(raw) < 2 || raw[0] != 0xFF || raw[1] != 0xD8 {
		t.Fatalf("not a JPEG: % x", raw[:min(4, len(raw))])
	}
	payload := []byte("Exif\x00\x00MM\x00\x2a\x00\x00\x00\x08")
	payload = append(payload, []byte("DateTimeOriginal 2023:03:14 08:31:00 GPSLatitude 37.7749")...)

	segment := []byte{0xFF, 0xE1}
	length := make([]byte, 2)
	binary.BigEndian.PutUint16(length, uint16(len(payload)+2))
	segment = append(segment, length...)
	segment = append(segment, payload...)

	spliced := make([]byte, 0, len(raw)+len(segment))
	spliced = append(spliced, raw[:2]...)
	spliced = append(spliced, segment...)
	spliced = append(spliced, raw[2:]...)
	return spliced
}

// 05-AC-7. The assertion is over the *bytes* rather than over a parsed EXIF structure, because the
// property being claimed is that nothing survived — not that one library can no longer find it.
func TestSanitizeStripsEXIFFromTheStoredFile(t *testing.T) {
	original := encodeJPEG(t, swatch(64, 48))
	tainted := withEXIF(t, original)

	// The fixture has to actually carry the thing, or this test proves nothing at all.
	if !bytes.Contains(tainted, []byte("2023:03:14")) {
		t.Fatal("the fixture does not contain a capture date; the test would pass vacuously")
	}

	sanitized, err := Sanitize(tainted, DefaultLimits)
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	for _, forbidden := range []string{"Exif", "2023:03:14", "08:31:00", "GPSLatitude", "37.7749"} {
		if bytes.Contains(sanitized.Bytes, []byte(forbidden)) {
			t.Errorf("the stored image still contains %q", forbidden)
		}
	}
	// The pixels are what was meant to survive.
	if sanitized.Width != 64 || sanitized.Height != 48 {
		t.Errorf("dimensions = %dx%d, want 64x48", sanitized.Width, sanitized.Height)
	}
	if sanitized.ContentType != "image/jpeg" {
		t.Errorf("content type = %s, want image/jpeg", sanitized.ContentType)
	}
}

func TestSanitizeStripsPNGTextChunks(t *testing.T) {
	var out bytes.Buffer
	if err := png.Encode(&out, swatch(32, 32)); err != nil {
		t.Fatalf("encode: %v", err)
	}
	raw := out.Bytes()
	// A tEXt chunk spliced after the IHDR, which is where a screenshot tool writes its software
	// name and timestamp.
	chunk := buildPNGChunk("tEXt", []byte("Creation Time\x002023-03-14T08:31:00Z"))
	insertAt := bytes.Index(raw, []byte("IDAT")) - 4
	if insertAt < 0 {
		t.Fatal("no IDAT chunk in the fixture")
	}
	tainted := append(append(append([]byte{}, raw[:insertAt]...), chunk...), raw[insertAt:]...)
	if !bytes.Contains(tainted, []byte("2023-03-14")) {
		t.Fatal("the fixture does not carry the timestamp; the test would pass vacuously")
	}

	sanitized, err := Sanitize(tainted, DefaultLimits)
	if err != nil {
		t.Fatalf("Sanitize: %v", err)
	}
	for _, forbidden := range []string{"Creation Time", "2023-03-14", "tEXt"} {
		if bytes.Contains(sanitized.Bytes, []byte(forbidden)) {
			t.Errorf("the stored image still contains %q", forbidden)
		}
	}
}

func TestSanitizeRefusesWhatIsNotAnImageItAccepts(t *testing.T) {
	cases := map[string][]byte{
		"an SVG document":     []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`),
		"a PDF":               []byte("%PDF-1.7\n1 0 obj\n<<>>\nendobj\n"),
		"a shell script":      []byte("#!/bin/sh\nrm -rf /\n"),
		"an empty file":       {},
		"a JPEG header alone": {0xFF, 0xD8, 0xFF, 0xE0},
	}
	for name, raw := range cases {
		if _, err := Sanitize(raw, DefaultLimits); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestSanitizeRefusesAnOversizedUpload(t *testing.T) {
	raw := encodeJPEG(t, swatch(64, 64))
	if _, err := Sanitize(raw, Limits{MaxBytes: int64(len(raw)) - 1, MaxPixels: DefaultLimits.MaxPixels}); err == nil {
		t.Error("an upload over the byte limit was accepted")
	}
	// A 50KB PNG can declare 40,000 x 40,000 and allocate gigabytes of pixels. The dimensions are
	// read from the header and refused before anything is decoded, so the limit is about what the
	// file claims rather than about what it costs to find out.
	if _, err := Sanitize(raw, Limits{MaxBytes: DefaultLimits.MaxBytes, MaxPixels: 100}); err == nil {
		t.Error("an image over the pixel limit was accepted")
	}
}

// The stored filename never comes from the upload: a client-supplied name is a path traversal
// waiting to happen, and also somewhere a trader could publish the window by calling their
// screenshot eurusd-2023-03-14.png.
func TestExtensionFollowsTheSniffedFormat(t *testing.T) {
	if got := Extension("image/jpeg"); got != ".jpg" {
		t.Errorf("Extension(image/jpeg) = %s", got)
	}
	if got := Extension("image/png"); got != ".png" {
		t.Errorf("Extension(image/png) = %s", got)
	}
	// Anything else falls back to .png rather than to whatever was asked for: the extension names
	// what the encoder produced, and an unknown type reaching here means the switch above it grew a
	// branch this did not.
	if got := Extension("application/x-msdownload"); got != ".png" {
		t.Errorf("an unknown type produced %s, want .png", got)
	}
}

func buildPNGChunk(kind string, data []byte) []byte {
	chunk := make([]byte, 0, len(data)+12)
	length := make([]byte, 4)
	binary.BigEndian.PutUint32(length, uint32(len(data)))
	chunk = append(chunk, length...)
	chunk = append(chunk, []byte(kind)...)
	chunk = append(chunk, data...)
	// A real CRC, over the type and the data. Go's decoder verifies checksums even on ancillary
	// chunks it then skips, so a fixture with a wrong one is rejected as a corrupt file and the
	// test would be asserting nothing about stripping.
	sum := make([]byte, 4)
	binary.BigEndian.PutUint32(sum, crc32.ChecksumIEEE(chunk[4:]))
	chunk = append(chunk, sum...)
	return chunk
}
