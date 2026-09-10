// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image/png"
	"io"
	"sync"
)

const (
	MaxImageBytes      = 8 << 20
	MaxImagePixels     = 4 << 20
	MaxImagePlacements = 256
	DefaultImageBudget = 64 << 20
)

var (
	ErrImageInvalid        = errors.New("invalid terminal image")
	ErrImageBudget         = errors.New("terminal image budget exceeded")
	ErrImageClosed         = errors.New("terminal image reference closed")
	ErrGraphicsUnsupported = errors.New("terminal graphics unsupported")
)

// ImageInfo is immutable resource metadata. ID is content identity, not authority.
// Encoded bytes never occur in Frame or Snapshot wire metadata.
type ImageInfo struct {
	ID     string
	Format string
	Width  int
	Height int
	Bytes  int
}

// ImageStore bounds resident encoded data plus its worst-case RGBA64 decode cost.
// References, including captures, share this budget. There is no lookup by ID:
// callers must already possess an Image or an authorized Capture.
type ImageStore struct {
	entries map[string]*imageData
	mu      sync.Mutex
	limit   int64
	used    int64
}
type imageData struct {
	encoded []byte
	info    ImageInfo
	refs    int
	charge  int64
}

// Image is one owned reference. Retain creates an independent reference; Close
// releases exactly once. Access and Close may run concurrently.
type Image struct {
	store *ImageStore
	data  *imageData
	mu    sync.Mutex
}

func NewImageStore(limit int64) *ImageStore {
	return &ImageStore{limit: max(0, limit), entries: make(map[string]*imageData)}
}

// ImportPNG is a native ingress seam for file readers and future Wasm codecs.
// Admission precedes full decoding and copying. No Lua pixel processing is needed.
func (s *ImageStore) ImportPNG(encoded []byte) (*Image, error) {
	if len(encoded) == 0 || len(encoded) > MaxImageBytes {
		return nil, ErrImageInvalid
	}
	config, err := png.DecodeConfig(bytes.NewReader(encoded))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > MaxImagePixels/config.Height {
		return nil, ErrImageInvalid
	}
	sum := sha256.Sum256(encoded)
	id := "sha256:" + hex.EncodeToString(sum[:])
	charge := int64(len(encoded)) + int64(config.Width)*int64(config.Height)*8
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.entries[id]; d != nil {
		d.refs++
		return &Image{store: s, data: d}, nil
	}
	if charge > s.limit-s.used {
		return nil, ErrImageBudget
	}
	// Decode validates the complete PNG, not merely an attacker-controlled header.
	if _, err := png.Decode(bytes.NewReader(encoded)); err != nil {
		return nil, ErrImageInvalid
	}
	d := &imageData{info: ImageInfo{ID: id, Format: "png", Width: config.Width, Height: config.Height, Bytes: len(encoded)}, encoded: bytes.Clone(encoded), refs: 1, charge: charge}
	s.entries[id] = d
	s.used += charge
	return &Image{store: s, data: d}, nil
}

func (s *ImageStore) Used() int64 { s.mu.Lock(); defer s.mu.Unlock(); return s.used }

func (i *Image) Info() (ImageInfo, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.data == nil {
		return ImageInfo{}, ErrImageClosed
	}
	return i.data.info, nil
}
func (i *Image) Retain() (*Image, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.data == nil {
		return nil, ErrImageClosed
	}
	i.store.mu.Lock()
	i.data.refs++
	i.store.mu.Unlock()
	return &Image{store: i.store, data: i.data}, nil
}
func (i *Image) ReadAt(p []byte, offset int64) (int, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.data == nil {
		return 0, ErrImageClosed
	}
	return bytes.NewReader(i.data.encoded).ReadAt(p, offset)
}
func (i *Image) WriteTo(w io.Writer) (int64, error) {
	// Keep a private reference while calling an external writer.
	ref, err := i.Retain()
	if err != nil {
		return 0, err
	}
	defer func() { _ = ref.Close() }()
	return bytes.NewReader(ref.data.encoded).WriteTo(w)
}
func (i *Image) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.data == nil {
		return nil
	}
	s, d := i.store, i.data
	i.data = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	d.refs--
	if d.refs == 0 {
		delete(s.entries, d.info.ID)
		s.used -= d.charge
	}
	return nil
}

// PixelRect uses zero-based source pixels; CellRect uses zero-based cells in Go.
// Lua projections translate destination cells to one-based coordinates.
type PixelRect struct{ X, Y, Width, Height int }
type CellRect struct{ X, Y, Cols, Rows int }

// Placement describes resolved display state, not a protocol transaction.
// Kind is currently "rect". Other kinds must be capability-gated before use.
// IDs are scoped to a producer. Image identity is separate from placement identity.
type Placement struct {
	ID          string
	Kind        string
	Alt         string
	Image       ImageInfo
	Destination CellRect
	Source      PixelRect
	Z           int32
}

// PlacedImage is native authoring input. Resource is an owned capability, never
// a string lookup. Present borrows it and retains its own reference on success.
type PlacedImage struct {
	Resource  *Image
	Placement Placement
}

// RetainPlacements validates a complete replacement before publication. Failure
// releases every temporary reference and leaves the previous frame unchanged.
func RetainPlacements(images []PlacedImage) ([]PlacedImage, error) {
	if len(images) == 0 {
		return nil, nil
	}
	if len(images) > MaxImagePlacements {
		return nil, ErrImageBudget
	}
	var out []PlacedImage
	fail := func(err error) ([]PlacedImage, error) { ClosePlacements(out); return nil, err }
	ids := make(map[string]bool, len(images))
	for _, item := range images {
		p := item.Placement
		if p.Kind == "" {
			p.Kind = "rect"
		}
		d := p.Destination
		if p.ID == "" || len(p.ID) > 128 || ids[p.ID] || p.Kind != "rect" || item.Resource == nil || len(p.Alt) > 256 ||
			d.X < -MaxViewportDimension || d.Y < -MaxViewportDimension || d.X > MaxViewportDimension || d.Y > MaxViewportDimension ||
			ValidateViewportSize(d.Cols, d.Rows) != nil {
			return fail(ErrImageInvalid)
		}
		ref, err := item.Resource.Retain()
		if err != nil {
			return fail(err)
		}
		info, _ := ref.Info()
		src := p.Source
		if src == (PixelRect{}) {
			src = PixelRect{Width: info.Width, Height: info.Height}
		}
		if src.X < 0 || src.Y < 0 || src.Width < 1 || src.Height < 1 || src.X > info.Width-src.Width || src.Y > info.Height-src.Height {
			_ = ref.Close()
			return fail(ErrImageInvalid)
		}
		p.Image, p.Source = info, src
		ids[p.ID] = true
		out = append(out, PlacedImage{Placement: p, Resource: ref})
	}
	return out, nil
}
func ClosePlacements(images []PlacedImage) {
	for _, i := range images {
		_ = i.Resource.Close()
	}
}
func PlacementMetadata(images []PlacedImage) []Placement {
	if len(images) == 0 {
		return nil
	}
	out := make([]Placement, len(images))
	for n, i := range images {
		out[n] = i.Placement
	}
	return out
}

// Capture pins the exact snapshot and all its resources. Its owner must Close.
// A later producer delete cannot invalidate a successfully acquired capture.
type Capture struct {
	images   []PlacedImage
	snapshot Snapshot
	mu       sync.Mutex
	closed   bool
}

func NewCapture(snapshot Snapshot, images []PlacedImage) (*Capture, error) {
	refs, err := RetainPlacements(images)
	if err != nil {
		return nil, err
	}
	snapshot.Rows = append([]string(nil), snapshot.Rows...)
	if snapshot.Cursor != nil {
		c := *snapshot.Cursor
		snapshot.Cursor = &c
	}
	snapshot.Images = PlacementMetadata(refs)
	return &Capture{snapshot: snapshot, images: refs}, nil
}
func (c *Capture) Snapshot() Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return Snapshot{}
	}
	s := c.snapshot
	s.Rows = append([]string(nil), s.Rows...)
	s.Images = append([]Placement(nil), s.Images...)
	if s.Cursor != nil {
		cursor := *s.Cursor
		s.Cursor = &cursor
	}
	return s
}
func (c *Capture) Image(id string) (*Image, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, ErrImageClosed
	}
	for _, p := range c.images {
		if p.Placement.Image.ID == id {
			return p.Resource.Retain()
		}
	}
	return nil, ErrPermissionDenied
}
func (c *Capture) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		ClosePlacements(c.images)
		c.images = nil
		c.snapshot = Snapshot{}
	}
	return nil
}

// CaptureViewport is optional so existing viewport implementations remain valid.
// Remote implementations may yield; ordinary Snapshot always stays nonblocking.
type CaptureViewport interface {
	Capture(context.Context) (*Capture, error)
}
