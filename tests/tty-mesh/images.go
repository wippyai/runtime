// SPDX-License-Identifier: MPL-2.0
package main

import (
	"bytes"
	"image"
	"image/png"
	"math/rand"
	"sync/atomic"

	"github.com/hashicorp/go-msgpack/v2/codec"
)

// A deterministic, poorly compressible PNG forces multiple native mesh chunks.
func proofImage() ([]byte, error) {
	rgba := image.NewNRGBA(image.Rect(0, 0, 256, 256))
	//nolint:gosec // Reproducible visual fixture, never keys or authorization tokens.
	_, _ = rand.New(rand.NewSource(17)).Read(rgba.Pix)
	var b bytes.Buffer
	err := png.Encode(&b, rgba)
	return b.Bytes(), err
}

type imageMetrics struct {
	bytes  atomic.Uint64
	chunks atomic.Uint64
}

func (m *imageMetrics) observe(data []byte) {
	var f struct{ Data []byte }
	h := codec.MsgpackHandle{}
	if codec.NewDecoderBytes(data, &h).Decode(&f) == nil && len(f.Data) > 0 {
		m.bytes.Add(uint64(len(f.Data)))
		m.chunks.Add(1)
	}
}

const imageChildScript = `
local events=assert(tty.events())
assert(tty.start())
local surface=assert(tty.surface())
local image=assert(tty.image(proof_image_png))
local sequence=0
local function paint()
 assert(surface:present({"IMAGE_READY:"..sequence},{images={{placement_id="chart",image=image,x=1+sequence%30,y=3,cols=32,rows=8,alt="Remote chart"}}}))
end
paint()
while true do
 local event,ok=events:receive()
 if not ok or event.type=="close" then break end
 if event.type=="key" then sequence=sequence+1 end
 paint()
end
assert(surface:close())
assert(image:close())
`
const imageAgentScript = `
local observer=assert(tty.attach(observe_ref))
local control=assert(tty.attach(input_ref))
local denied,err=control:capture()
assert(denied==nil and err,"input-only mount gained image observation")
local updates=assert(observer:updates())
local function wait_for(sequence)
 while true do
  local snap=assert(observer:snapshot())
  if snap.rows[1]=="IMAGE_READY:"..sequence then return snap end
  local _,open=updates:receive();assert(open,"observer closed")
 end
end
wait_for(0)
local first=assert(observer:capture())
local info=first:snapshot().images[1]
local held=assert(first:image(info.image_id))
assert(held:read()==proof_image_png,"remote PNG differs from producer")
assert(first:close())
assert(control:resize(100,30))
for i=1,commands do
 measure("start")
 assert(control:send({type="key",key="right",key_type="right",action="press"}))
 wait_for(i)
 local capture=assert(observer:capture())
 local snap=capture:snapshot()
 assert(snap.width==100 and snap.height==30 and snap.layers[2]=="images")
 assert(snap.images[1].x==1+i%30)
 local image=assert(capture:image(snap.images[1].image_id))
 assert(image:read()==proof_image_png)
 assert(image:close());assert(capture:close())
 measure("end")
end
assert(control:close());assert(observer:close())
local gone,closed=observer:capture();assert(gone==nil and closed)
-- A retained resource survives detach; no new remote authority is created.
assert(held:read()==proof_image_png);assert(held:close())
return "mesh Lua image proof passed"
`
