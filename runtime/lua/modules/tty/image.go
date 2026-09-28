// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"fmt"
	"math"
	"sync"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const imageTypeName = "tty.Image"
const captureTypeName = "tty.Capture"

type imageWrapper struct {
	image  *ttyapi.Image
	cancel func()
	once   sync.Once
}
type captureWrapper struct {
	capture *ttyapi.Capture
	cancel  func()
	once    sync.Once
}

func init() {
	value.RegisterTypeMethods(nil, imageTypeName, map[string]lua.LGoFunc{"__gc": imageGC}, map[string]lua.LGoFunc{"info": imageInfo, "read": imageRead, "close": imageClose})
	value.RegisterTypeMethods(nil, captureTypeName, map[string]lua.LGoFunc{"__gc": captureGC}, map[string]lua.LGoFunc{"snapshot": captureSnapshot, "image": captureImage, "close": captureClose})
}
func pushImage(l *lua.LState, img *ttyapi.Image, cleanup ...func()) {
	h := &imageWrapper{image: img}
	if len(cleanup) > 0 && cleanup[0] != nil {
		h.cancel = cleanup[0]
	} else if store := resource.GetStore(l.Context()); store != nil {
		h.cancel = store.AddCleanup(img.Close)
	}
	value.PushTypedUserData(l, h, imageTypeName)
	l.Push(lua.LNil)
}
func pushCapture(l *lua.LState, capture *ttyapi.Capture, cleanup ...func()) {
	h := &captureWrapper{capture: capture}
	if len(cleanup) > 0 && cleanup[0] != nil {
		h.cancel = cleanup[0]
	} else if store := resource.GetStore(l.Context()); store != nil {
		h.cancel = store.AddCleanup(capture.Close)
	}
	value.PushTypedUserData(l, h, captureTypeName)
	l.Push(lua.LNil)
}
func checkImage(l *lua.LState) *imageWrapper {
	if h, ok := l.CheckUserData(1).Value.(*imageWrapper); ok {
		return h
	}
	l.ArgError(1, "tty.Image expected")
	return nil
}
func checkCapture(l *lua.LState) *captureWrapper {
	if h, ok := l.CheckUserData(1).Value.(*captureWrapper); ok {
		return h
	}
	l.ArgError(1, "tty.Capture expected")
	return nil
}
func (h *imageWrapper) close() {
	h.once.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
		_ = h.image.Close()
	})
}
func (h *captureWrapper) close() {
	h.once.Do(func() {
		if h.cancel != nil {
			h.cancel()
		}
		_ = h.capture.Close()
	})
}
func imageGC(l *lua.LState) int      { checkImage(l).close(); return 0 }
func captureGC(l *lua.LState) int    { checkCapture(l).close(); return 0 }
func imageClose(l *lua.LState) int   { checkImage(l).close(); l.Push(lua.LTrue); return 1 }
func captureClose(l *lua.LState) int { checkCapture(l).close(); l.Push(lua.LTrue); return 1 }
func imageError(l *lua.LState, err error) int {
	l.Push(lua.LNil)
	l.Push(lua.WrapErrorWithLua(l, err, "terminal image"))
	return 2
}
func ttyImageNew(l *lua.LState) int {
	data := l.CheckString(1)
	if len(data) == 0 || len(data) > ttyapi.MaxImageBytes {
		return imageError(l, ttyapi.ErrImageInvalid)
	}
	l.Push(&ViewportIOYield{Command: ttyapi.ViewportIOCmd{Operation: "image_import", ImageData: []byte(data)}})
	return -1
}
func imageInfoTable(l *lua.LState, info ttyapi.ImageInfo) *lua.LTable {
	t := l.CreateTable(0, 5)
	t.RawSetString("id", lua.LString(info.ID))
	t.RawSetString("format", lua.LString(info.Format))
	t.RawSetString("width", lua.LInteger(info.Width))
	t.RawSetString("height", lua.LInteger(info.Height))
	t.RawSetString("bytes", lua.LInteger(info.Bytes))
	t.Immutable = true
	return t
}
func imageInfo(l *lua.LState) int {
	info, err := checkImage(l).image.Info()
	if err != nil {
		return imageError(l, err)
	}
	l.Push(imageInfoTable(l, info))
	return 1
}

// read is explicit bounded export I/O, never the compositor or mesh data path.
func imageRead(l *lua.LState) int {
	img := checkImage(l).image
	info, err := img.Info()
	if err != nil {
		return imageError(l, err)
	}
	data := make([]byte, info.Bytes)
	if _, err := img.ReadAt(data, 0); err != nil {
		return imageError(l, err)
	}
	l.Push(lua.LString(data))
	return 1
}
func captureImage(l *lua.LState) int {
	img, err := checkCapture(l).capture.Image(l.CheckString(2))
	if err != nil {
		return imageError(l, err)
	}
	pushImage(l, img)
	return 2
}
func captureSnapshot(l *lua.LState) int {
	l.Push(snapshotTable(l, checkCapture(l).capture.Snapshot()))
	return 1
}
func viewportCapture(l *lua.LState) int {
	v := checkViewport(l).view
	if !checkViewportRight(l, v, ttyapi.RightObserve) {
		return 2
	}
	capture, ok := v.(ttyapi.CaptureViewport)
	if !ok {
		return imageError(l, ttyapi.ErrGraphicsUnsupported)
	}
	l.Push(&ViewportIOYield{Command: ttyapi.ViewportIOCmd{Operation: "capture", CaptureSource: capture}})
	return -1
}
func snapshotTable(l *lua.LState, s ttyapi.Snapshot) *lua.LTable {
	t := l.CreateTable(0, 8)
	rows := l.CreateTable(len(s.Rows), 0)
	for i, row := range s.Rows {
		rows.RawSetInt(i+1, lua.LString(row))
	}
	t.RawSetString("rows", rows)
	t.RawSetString("revision", lua.LInteger(s.Revision))
	t.RawSetString("width", lua.LInteger(s.Width))
	t.RawSetString("height", lua.LInteger(s.Height))
	if s.Cursor != nil {
		c := l.CreateTable(0, 3)
		c.RawSetString("x", lua.LInteger(s.Cursor.Column+1))
		c.RawSetString("y", lua.LInteger(s.Cursor.Row+1))
		c.RawSetString("visible", lua.LBool(s.Cursor.Visible))
		t.RawSetString("cursor", c)
	}
	addSnapshotImages(l, t, s)
	return t
}
func addSnapshotImages(l *lua.LState, t *lua.LTable, s ttyapi.Snapshot) {
	images := l.CreateTable(len(s.Images), 0)
	for i, p := range s.Images {
		row := l.CreateTable(0, 10)
		row.RawSetString("placement_id", lua.LString(p.ID))
		row.RawSetString("image_id", lua.LString(p.Image.ID))
		row.RawSetString("kind", lua.LString(p.Kind))
		row.RawSetString("x", lua.LInteger(p.Destination.X+1))
		row.RawSetString("y", lua.LInteger(p.Destination.Y+1))
		row.RawSetString("cols", lua.LInteger(p.Destination.Cols))
		row.RawSetString("rows", lua.LInteger(p.Destination.Rows))
		row.RawSetString("z", lua.LInteger(p.Z))
		row.RawSetString("alt", lua.LString(p.Alt))
		row.RawSetString("resource", imageInfoTable(l, p.Image))
		src := l.CreateTable(0, 4)
		src.RawSetString("x", lua.LInteger(p.Source.X))
		src.RawSetString("y", lua.LInteger(p.Source.Y))
		src.RawSetString("width", lua.LInteger(p.Source.Width))
		src.RawSetString("height", lua.LInteger(p.Source.Height))
		row.RawSetString("src", src)
		images.RawSetInt(i+1, row)
	}
	t.RawSetString("images", images)
	layers := l.CreateTable(2, 0)
	layers.RawSetInt(1, lua.LString("text"))
	if len(s.Images) > 0 {
		layers.RawSetInt(2, lua.LString("images"))
	}
	t.RawSetString("layers", layers)
	t.RawSetString("images_omitted", lua.LBool(s.ImagesOmitted))
}
func placedImagesFromLua(v lua.LValue) ([]ttyapi.PlacedImage, error) {
	if v == lua.LNil {
		return nil, nil
	}
	table, ok := v.(*lua.LTable)
	if !ok || table.Len() > ttyapi.MaxImagePlacements {
		return nil, ttyapi.ErrImageInvalid
	}
	images := make([]ttyapi.PlacedImage, 0, table.Len())
	for i := 1; i <= table.Len(); i++ {
		t, ok := table.RawGetInt(i).(*lua.LTable)
		if !ok {
			return nil, ttyapi.ErrImageInvalid
		}
		ud, ok := t.RawGetString("image").(*lua.LUserData)
		if !ok {
			return nil, ttyapi.ErrImageInvalid
		}
		img, ok := ud.Value.(*imageWrapper)
		if !ok {
			return nil, ttyapi.ErrImageInvalid
		}
		id, ok := t.RawGetString("placement_id").(lua.LString)
		if !ok {
			return nil, ttyapi.ErrImageInvalid
		}
		p := ttyapi.Placement{ID: string(id), Kind: "rect"}
		for _, f := range []struct {
			dest *int
			name string
		}{{&p.Destination.X, "x"}, {&p.Destination.Y, "y"}, {&p.Destination.Cols, "cols"}, {&p.Destination.Rows, "rows"}} {
			n, ok := integerValue(t.RawGetString(f.name))
			if !ok || n < -ttyapi.MaxViewportDimension || n > ttyapi.MaxViewportDimension {
				return nil, fmt.Errorf("image %s must be a bounded cell coordinate", f.name)
			}
			*f.dest = n
		}
		p.Destination.X--
		p.Destination.Y--
		if z := t.RawGetString("z"); z != lua.LNil {
			n, ok := integerValue(z)
			if !ok || n < math.MinInt32 || n > math.MaxInt32 {
				return nil, ttyapi.ErrImageInvalid
			}
			p.Z = int32(n)
		}
		if alt := t.RawGetString("alt"); alt != lua.LNil {
			a, ok := alt.(lua.LString)
			if !ok {
				return nil, ttyapi.ErrImageInvalid
			}
			p.Alt = string(a)
		}
		if crop := t.RawGetString("src"); crop != lua.LNil {
			c, ok := crop.(*lua.LTable)
			if !ok {
				return nil, ttyapi.ErrImageInvalid
			}
			for _, f := range []struct {
				dest *int
				name string
			}{{&p.Source.X, "x"}, {&p.Source.Y, "y"}, {&p.Source.Width, "width"}, {&p.Source.Height, "height"}} {
				n, ok := integerValue(c.RawGetString(f.name))
				if !ok || n < 0 || n > ttyapi.MaxImagePixels {
					return nil, ttyapi.ErrImageInvalid
				}
				*f.dest = n
			}
		}
		images = append(images, ttyapi.PlacedImage{Resource: img.image, Placement: p})
	}
	return images, nil
}
