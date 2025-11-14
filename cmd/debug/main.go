package main

import (
	"context"
	"fmt"
	"os"

	ctxapi "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/boot/build"
	"github.com/ponyruntime/pony/boot/build/stages"
	"github.com/ponyruntime/pony/boot/hash"
	"github.com/ponyruntime/pony/boot/loader"
	"github.com/ponyruntime/pony/boot/loader/interpolate"
	"github.com/ponyruntime/pony/boot/pack"
	"github.com/ponyruntime/pony/deps/lock"
	transcoder "github.com/ponyruntime/pony/system/payload"
	json2 "github.com/ponyruntime/pony/system/payload/json"
	"github.com/ponyruntime/pony/system/payload/lua"
	"github.com/ponyruntime/pony/system/payload/yaml"
)

func main() {
	os.Chdir("../be-common-components")

	ctx := context.Background()
	ctx = ctxapi.WithAppContext(ctx, ctxapi.NewAppContext())

	dtt := transcoder.GlobalTranscoder()
	json2.Register(dtt)
	yaml.Register(dtt)
	lua.Register(dtt)
	ctx = payload.WithTranscoder(ctx, dtt)

	ldr := loader.NewLoader(dtt, nil, interpolate.NewEntryInterpolator(dtt))

	lockPath := "wippy.lock"
	lockObj, err := lock.New(lockPath)
	if err != nil {
		panic(err)
	}

	paths := lockObj.GetLoadPaths()
	entries := []regapi.Entry{}
	for _, path := range paths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			continue
		}
		dirFS := os.DirFS(path)
		loaded, err := ldr.LoadFS(ctx, dirFS)
		if err != nil {
			panic(err)
		}
		entries = append(entries, loaded...)
	}

	fmt.Printf("Loaded %d entries\n", len(entries))

	// Hash before pipeline
	hasher := hash.New(dtt)
	hashBefore, _ := hasher.Hash(entries)
	fmt.Printf("Hash before pipeline: %s\n", hashBefore)

	// Run pipeline
	pipeline := build.New(
		stages.Override(),
		stages.Disable(),
		stages.Link(),
	)
	pipeline.Execute(ctx, &entries)

	// Hash after pipeline
	hashAfter, _ := hasher.Hash(entries)
	fmt.Printf("Hash after pipeline:  %s\n", hashAfter)
	fmt.Printf("Hashes match: %v\n", hashBefore == hashAfter)

	// Pack
	packer := pack.New(dtt)
	err = packer.Pack(entries, "debug.wapp")
	if err != nil {
		panic(err)
	}
	fmt.Println("Packed successfully")

	// Unpack
	unpacked, err := packer.Unpack("debug.wapp")
	if err != nil {
		fmt.Printf("Unpack error: %v\n", err)
	} else {
		fmt.Printf("Successfully unpacked %d entries\n", len(unpacked))
	}
}
