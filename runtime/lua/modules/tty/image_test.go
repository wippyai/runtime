// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/runtime/resource"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func luaImageFixture(t *testing.T) (*ttyapi.ImageStore, *ttyapi.Image) {
	t.Helper()
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, image.NewNRGBA(image.Rect(0, 0, 4, 4))))
	store := ttyapi.NewImageStore(ttyapi.DefaultImageBudget)
	img, err := store.ImportPNG(b.Bytes())
	require.NoError(t, err)
	return store, img
}
func TestLuaImageCaptureAndTypedPlacement(t *testing.T) {
	store, img := luaImageFixture(t)
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	pushImage(l, img)
	l.Pop(1)
	l.SetGlobal("image", l.Get(-1))
	l.Pop(1)
	require.NoError(t, l.DoString(`
 local info=assert(image:info());assert(info.format=="png" and info.width==4)
 assert(#image:read()==info.bytes)
 placements={{placement_id="p",image=image,x=2,y=3,cols=4,rows=2,src={x=0,y=0,width=2,height=2},z=-1}}
 `))
	placements, err := placedImagesFromLua(l.GetGlobal("placements"))
	require.NoError(t, err)
	require.Equal(t, 1, placements[0].Placement.Destination.X)
	capture, err := ttyapi.NewCapture(ttyapi.Snapshot{Rows: []string{"hello"}, Revision: 7}, placements)
	require.NoError(t, err)
	pushCapture(l, capture)
	l.Pop(1)
	l.SetGlobal("capture", l.Get(-1))
	l.Pop(1)
	require.NoError(t, l.DoString(`
 assert(image:close())
 local snap=capture:snapshot();assert(snap.revision==7 and snap.layers[2]=="images")
 assert(snap.images[1].x==2 and snap.images[1].src.width==2)
 local wrong,err=capture:image("foreign");assert(wrong==nil and err~=nil)
 local pinned=assert(capture:image(snap.images[1].image_id))
 assert(capture:close());assert(#pinned:read()>0);assert(pinned:close())
 local gone,closed=image:read();assert(gone==nil and closed~=nil)
 `))
	require.Zero(t, store.Used())
}
func TestLuaImageRejectsForgedHandleAndFrameCleanup(t *testing.T) {
	store, img := luaImageFixture(t)
	resources := resource.NewStore()
	defer resources.Close()
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer frame.Close()
	require.NoError(t, resource.SetStore(ctx, resources))
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)
	bindTTY(l)
	pushImage(l, img)
	l.Pop(1)
	l.SetGlobal("image", l.Get(-1))
	l.Pop(1)
	require.NoError(t, l.DoString(`bad={{placement_id="p",image="sha256:forged",x=1,y=1,cols=1,rows=1}}`))
	_, err := placedImagesFromLua(l.GetGlobal("bad"))
	require.Error(t, err)
	require.NoError(t, resources.Close())
	require.Zero(t, store.Used())
}
