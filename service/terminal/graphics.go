// SPDX-License-Identifier: MPL-2.0
package terminal

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/charmbracelet/x/ansi"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

var nextGraphicsID atomic.Uint32

// graphicsState tracks only host encoding state. Resources belong to the input
// frame while encoding; this backend never needs to keep pixel data afterward.
type graphicsState struct {
	known      map[string]uint32
	placements map[string]hostPlacement
}
type hostPlacement struct {
	placement            ttyapi.Placement
	imageID, placementID uint32
}

func graphicsID() (uint32, error) {
	for {
		n := nextGraphicsID.Load()
		if n >= 0xfffffeff {
			return 0, ttyapi.ErrImageBudget
		}
		if nextGraphicsID.CompareAndSwap(n, n+1) {
			return n + 1, nil
		}
	}
}

func deleteHostImage(out []byte, id uint32) []byte {
	return fmt.Appendf(out, "\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id)
}
func deleteHostPlacement(out []byte, p hostPlacement) []byte {
	return fmt.Appendf(out, "\x1b_Ga=d,d=p,i=%d,p=%d,q=2\x1b\\", p.imageID, p.placementID)
}

func (s *Surface) appendGraphics(out []byte, images []ttyapi.PlacedImage, enabled bool) ([]byte, map[string]hostPlacement, error) {
	if s.graphics.known == nil {
		s.graphics.known = make(map[string]uint32)
	}
	desired := make(map[string]hostPlacement, len(images))
	wanted := make(map[string]bool, len(images))
	if s.invalid {
		for _, id := range s.graphics.known {
			out = deleteHostImage(out, id)
		}
	}
	if enabled {
		for _, item := range images {
			p := item.Placement
			// Composition must clip at the destination viewport before host encoding.
			if p.Destination.X < 0 || p.Destination.Y < 0 {
				return nil, nil, ttyapi.ErrImageInvalid
			}
			imageID, known := s.graphics.known[p.Image.ID]
			if !known {
				if len(s.graphics.known) >= 2*ttyapi.MaxImagePlacements {
					return nil, nil, ttyapi.ErrImageBudget
				}
				var err error
				imageID, err = graphicsID()
				if err != nil {
					return nil, nil, err
				}
				s.graphics.known[p.Image.ID] = imageID
			}
			if (!known || s.invalid) && !wanted[p.Image.ID] {
				var err error
				out, err = appendImageUpload(out, item.Resource, imageID)
				if err != nil {
					return nil, nil, err
				}
			}
			wanted[p.Image.ID] = true
			previous, exists := s.graphics.placements[p.ID]
			if exists && previous.placement == p && !s.invalid {
				desired[p.ID] = previous
				continue
			}
			placementID := previous.placementID
			if !exists {
				var err error
				placementID, err = graphicsID()
				if err != nil {
					return nil, nil, err
				}
			}
			if exists && !s.invalid {
				out = deleteHostPlacement(out, previous)
			}
			out = fmt.Appendf(out, "\x1b[%d;%dH\x1b_Ga=p,i=%d,p=%d,c=%d,r=%d,x=%d,y=%d,w=%d,h=%d,z=%d,C=1,q=2\x1b\\",
				p.Destination.Y+1, p.Destination.X+1, imageID, placementID, p.Destination.Cols, p.Destination.Rows, p.Source.X, p.Source.Y, p.Source.Width, p.Source.Height, p.Z)
			desired[p.ID] = hostPlacement{placement: p, imageID: imageID, placementID: placementID}
		}
	}
	for key, old := range s.graphics.placements {
		if _, ok := desired[key]; !ok && !s.invalid {
			out = deleteHostPlacement(out, old)
		}
	}
	for hash, id := range s.graphics.known {
		if !wanted[hash] {
			out = deleteHostImage(out, id)
		}
	}
	return out, desired, nil
}
func appendImageUpload(out []byte, img *ttyapi.Image, id uint32) ([]byte, error) {
	info, err := img.Info()
	if err != nil {
		return nil, err
	}
	// 3072 raw bytes encode to kitty's maximum 4096-byte payload chunk.
	raw := make([]byte, 3072)
	encoded := make([]byte, 4096)
	for offset := 0; offset < info.Bytes; {
		n, err := img.ReadAt(raw, int64(offset))
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		if n == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		more := 0
		if offset+n < info.Bytes {
			more = 1
		}
		if offset == 0 {
			out = fmt.Appendf(out, "\x1b_Ga=t,f=100,i=%d,q=2,m=%d;", id, more)
		} else {
			out = fmt.Appendf(out, "\x1b_Gm=%d;", more)
		}
		base64.StdEncoding.Encode(encoded, raw[:n])
		out = append(out, encoded[:base64.StdEncoding.EncodedLen(n)]...)
		out = append(out, "\x1b\\"...)
		offset += n
	}
	return out, nil
}
func (s *Surface) commitGraphics(placements map[string]hostPlacement) {
	s.graphics.placements = placements
	wanted := make(map[string]bool, len(placements))
	for _, p := range placements {
		wanted[p.placement.Image.ID] = true
	}
	for hash := range s.graphics.known {
		if !wanted[hash] {
			delete(s.graphics.known, hash)
		}
	}
}
func placeholderRows(rows []string, images []ttyapi.PlacedImage, width, height int) ([]string, error) {
	desiredHeight := len(rows)
	inferredWidth := 0
	for _, row := range rows {
		inferredWidth = max(inferredWidth, ansi.StringWidth(row))
	}
	for _, item := range images {
		d := item.Placement.Destination
		desiredHeight = max(desiredHeight, d.Y+d.Rows)
		inferredWidth = max(inferredWidth, d.X+d.Cols)
	}
	if width <= 0 {
		width = inferredWidth
	}
	if height <= 0 {
		height = desiredHeight
	}
	if ttyapi.ValidateViewportSize(width, height) != nil {
		return nil, ttyapi.ErrInvalidViewportSize
	}
	out := slices.Clone(rows)
	for len(out) < min(height, desiredHeight) {
		out = append(out, "")
	}

	for _, item := range images {
		p := item.Placement
		d := p.Destination
		left, right := max(0, d.X), min(width, d.X+d.Cols)
		if left >= right {
			continue
		}
		label := "[image]"
		if p.Alt != "" {
			label = "[image: " + ansi.Strip(p.Alt) + "]"
		}
		// Strip C0/C1 controls that are not escape sequences from alternative text.
		label = strings.Map(func(r rune) rune {
			if r < 32 || (r >= 127 && r <= 159) {
				return -1
			}
			return r
		}, label)
		for y := max(0, d.Y); y < min(len(out), d.Y+d.Rows); y++ {
			text := strings.Repeat("▄", right-left)
			if y == max(0, d.Y) {
				text = ansi.Truncate(label, right-left, "")
				text += strings.Repeat(" ", right-left-ansi.StringWidth(text))
			}
			prefix := ansi.Cut(out[y], 0, left)
			prefix += strings.Repeat(" ", max(0, left-ansi.StringWidth(prefix)))
			out[y] = prefix + "\x1b[0;2m" + text + "\x1b[0m" + ansi.Cut(out[y], right, width)
		}
	}
	return out, nil
}
