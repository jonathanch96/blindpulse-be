// Package media takes an image a trader uploaded and returns one that cannot say anything about
// them or about the market window.
//
// The rule it exists to enforce is a blinding guard, not a hygiene one. A screenshot carries an
// EXIF capture timestamp, and often GPS. A session that withholds the date from every payload for
// eight hundred bars is undone by one screenshot that says "taken 14 March 2023, 08:31" — and the
// leak comes from inside the trader's own upload, which nothing else in the system has to defend
// against.
package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"net/http"
)

// Limits bound what a journal entry may carry.
type Limits struct {
	// MaxBytes caps the encoded upload.
	MaxBytes int64
	// MaxPixels caps width × height *after* decoding the header and before decoding the pixels.
	// A 50KB PNG can declare 40,000 × 40,000, which allocates 6.4GB of pixels and takes the process
	// down — so the dimensions are read first and rejected before anything is materialized.
	MaxPixels int64
}

// DefaultLimits are sized for a chart screenshot on a large monitor, with room to spare. A 5MB,
// 30-megapixel ceiling admits everything a trader would legitimately attach and nothing that looks
// like an attempt to store a video.
var DefaultLimits = Limits{MaxBytes: 5 << 20, MaxPixels: 30_000_000}

// Sanitized is an image stripped of everything except its pixels.
type Sanitized struct {
	Bytes       []byte
	ContentType string
	Width       int
	Height      int
}

var (
	// ErrUnsupportedType is returned for anything that is not a JPEG or a PNG. The list is short on
	// purpose: every format admitted is another decoder's worth of parsing surface, and a trader
	// attaching a chart screenshot needs neither TIFF nor SVG. SVG in particular is a document that
	// can carry script and fetch remote resources, so it would be the worst possible thing to
	// accept and hand back under a signed URL.
	ErrUnsupportedType = errors.New("media: only JPEG and PNG are accepted")
	ErrTooLarge        = errors.New("media: upload exceeds the size limit")
	ErrTooManyPixels   = errors.New("media: image dimensions exceed the limit")
	ErrUndecodable     = errors.New("media: the file could not be decoded as the image it claims to be")
)

// Sanitize decodes an upload and re-encodes it from its pixels.
//
// Re-encoding rather than stripping metadata segments is the decision worth stating. A segment
// stripper has to know every marker that can carry metadata — EXIF in APP1, XMP in another APP1,
// IPTC in APP13, ICC in APP2, PNG's tEXt/iTXt/zTXt/eXIf/tIME — and a format that gains one, or a
// file that hides a payload in a place the stripper does not walk, gets through. Decoding to pixels
// and encoding fresh drops everything by construction: what comes out is a function of the image,
// and nothing else survives to be examined.
//
// The cost is a re-compression, which for a JPEG screenshot is lossy over lossy. That is a real
// cost and it is accepted: a slightly softer screenshot is worth more than a date the trader did
// not mean to publish.
func Sanitize(raw []byte, limits Limits) (*Sanitized, error) {
	if limits.MaxBytes <= 0 {
		limits = DefaultLimits
	}
	if int64(len(raw)) > limits.MaxBytes {
		return nil, fmt.Errorf("%w: %d bytes", ErrTooLarge, len(raw))
	}
	// The declared content type is ignored entirely — a client controls it, so it says what the
	// uploader wants believed rather than what the bytes are. This sniffs the actual header.
	declared := http.DetectContentType(raw)
	if declared != "image/jpeg" && declared != "image/png" {
		return nil, fmt.Errorf("%w: got %s", ErrUnsupportedType, declared)
	}

	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUndecodable
	}
	if int64(config.Width)*int64(config.Height) > limits.MaxPixels {
		return nil, fmt.Errorf("%w: %dx%d", ErrTooManyPixels, config.Width, config.Height)
	}

	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, ErrUndecodable
	}

	var out bytes.Buffer
	contentType := "image/png"
	switch format {
	case "jpeg":
		contentType = "image/jpeg"
		// Quality 85 is the usual "you cannot see the difference on a screenshot" setting, and it
		// keeps a re-encoded chart from growing larger than the file that arrived.
		if err := jpeg.Encode(&out, decoded, &jpeg.Options{Quality: 85}); err != nil {
			return nil, ErrUndecodable
		}
	case "png":
		if err := png.Encode(&out, decoded); err != nil {
			return nil, ErrUndecodable
		}
	default:
		// Unreachable while the sniff above admits two formats, and kept so that widening that list
		// without widening this switch fails loudly rather than writing an unencoded file.
		return nil, ErrUnsupportedType
	}

	bounds := decoded.Bounds()
	return &Sanitized{
		Bytes:       out.Bytes(),
		ContentType: contentType,
		Width:       bounds.Dx(),
		Height:      bounds.Dy(),
	}, nil
}

// Extension names the file on disk from its content type. The stored name never comes from the
// upload: a client-supplied filename is a path traversal waiting to happen, and it is also a place
// a trader could leak the date by calling their screenshot `eurusd-2023-03-14.png`.
func Extension(contentType string) string {
	if contentType == "image/jpeg" {
		return ".jpg"
	}
	return ".png"
}
