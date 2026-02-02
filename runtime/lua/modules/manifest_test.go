package modules_test

import (
	"testing"

	"github.com/wippyai/runtime/runtime/lua/modules/base64"
	"github.com/wippyai/runtime/runtime/lua/modules/cloudstorage"
	"github.com/wippyai/runtime/runtime/lua/modules/crypto"
	"github.com/wippyai/runtime/runtime/lua/modules/ctx"
	"github.com/wippyai/runtime/runtime/lua/modules/excel"
	"github.com/wippyai/runtime/runtime/lua/modules/httpclient"
	"github.com/wippyai/runtime/runtime/lua/modules/metrics"
	"github.com/wippyai/runtime/runtime/lua/modules/payload"
	"github.com/wippyai/runtime/runtime/lua/modules/queue"
	"github.com/wippyai/runtime/runtime/lua/modules/store"
	"github.com/wippyai/runtime/runtime/lua/modules/websocket"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/go-lua/types/typ"
)

type methodCheck struct {
	name     string
	manifest *io.Manifest
	typeName string
	method   string
}

type recordFieldCheck struct {
	name     string
	manifest *io.Manifest
	field    string
	method   string
}

func TestModuleTypes_ReturnsWithError(t *testing.T) {
	checks := []methodCheck{
		{name: "queue.Message.id", manifest: queue.ModuleTypes(), typeName: "Message", method: "id"},
		{name: "queue.Message.header", manifest: queue.ModuleTypes(), typeName: "Message", method: "header"},
		{name: "queue.Message.headers", manifest: queue.ModuleTypes(), typeName: "Message", method: "headers"},
		{name: "queue.publish", manifest: queue.ModuleTypes(), method: "publish"},
		{name: "queue.message", manifest: queue.ModuleTypes(), method: "message"},
		{name: "websocket.Client.send", manifest: websocket.ModuleTypes(), typeName: "Client", method: "send"},
		{name: "websocket.Client.receive", manifest: websocket.ModuleTypes(), typeName: "Client", method: "receive"},
		{name: "websocket.Client.channel", manifest: websocket.ModuleTypes(), typeName: "Client", method: "channel"},
		{name: "websocket.Client.close", manifest: websocket.ModuleTypes(), typeName: "Client", method: "close"},
		{name: "websocket.Client.ping", manifest: websocket.ModuleTypes(), typeName: "Client", method: "ping"},
		{name: "websocket.connect", manifest: websocket.ModuleTypes(), method: "connect"},
		{name: "cloudstorage.Storage.list_objects", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "list_objects"},
		{name: "cloudstorage.Storage.download_object", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "download_object"},
		{name: "cloudstorage.Storage.upload_object", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "upload_object"},
		{name: "cloudstorage.Storage.delete_objects", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "delete_objects"},
		{name: "cloudstorage.Storage.presigned_get_url", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "presigned_get_url"},
		{name: "cloudstorage.Storage.presigned_put_url", manifest: cloudstorage.ModuleTypes(), typeName: "Storage", method: "presigned_put_url"},
		{name: "cloudstorage.get", manifest: cloudstorage.ModuleTypes(), method: "get"},
		{name: "ctx.get", manifest: ctx.ModuleTypes(), method: "get"},
		{name: "ctx.all", manifest: ctx.ModuleTypes(), method: "all"},
		{name: "excel.Workbook.new_sheet", manifest: excel.ModuleTypes(), typeName: "Workbook", method: "new_sheet"},
		{name: "excel.Workbook.get_sheet_list", manifest: excel.ModuleTypes(), typeName: "Workbook", method: "get_sheet_list"},
		{name: "excel.Workbook.get_rows", manifest: excel.ModuleTypes(), typeName: "Workbook", method: "get_rows"},
		{name: "excel.new", manifest: excel.ModuleTypes(), method: "new"},
		{name: "excel.open", manifest: excel.ModuleTypes(), method: "open"},
		{name: "http_client.StreamReader.read", manifest: httpclient.ModuleTypes(), typeName: "StreamReader", method: "read"},
		{name: "http_client.StreamReader.close", manifest: httpclient.ModuleTypes(), typeName: "StreamReader", method: "close"},
		{name: "http_client.get", manifest: httpclient.ModuleTypes(), method: "get"},
		{name: "http_client.post", manifest: httpclient.ModuleTypes(), method: "post"},
		{name: "http_client.put", manifest: httpclient.ModuleTypes(), method: "put"},
		{name: "http_client.delete", manifest: httpclient.ModuleTypes(), method: "delete"},
		{name: "http_client.head", manifest: httpclient.ModuleTypes(), method: "head"},
		{name: "http_client.patch", manifest: httpclient.ModuleTypes(), method: "patch"},
		{name: "http_client.request", manifest: httpclient.ModuleTypes(), method: "request"},
		{name: "http_client.request_batch", manifest: httpclient.ModuleTypes(), method: "request_batch"},
		{name: "http_client.decode_uri", manifest: httpclient.ModuleTypes(), method: "decode_uri"},
		{name: "store.Store.get", manifest: store.ModuleTypes(), typeName: "Store", method: "get"},
		{name: "store.Store.set", manifest: store.ModuleTypes(), typeName: "Store", method: "set"},
		{name: "store.Store.delete", manifest: store.ModuleTypes(), typeName: "Store", method: "delete"},
		{name: "store.Store.has", manifest: store.ModuleTypes(), typeName: "Store", method: "has"},
		{name: "store.get", manifest: store.ModuleTypes(), method: "get"},
		{name: "payload.Payload.data", manifest: payload.ModuleTypes(), typeName: "Payload", method: "data"},
		{name: "payload.Payload.unmarshal", manifest: payload.ModuleTypes(), typeName: "Payload", method: "unmarshal"},
		{name: "payload.Payload.transcode", manifest: payload.ModuleTypes(), typeName: "Payload", method: "transcode"},
		{name: "base64.encode", manifest: base64.ModuleTypes(), method: "encode"},
		{name: "base64.decode", manifest: base64.ModuleTypes(), method: "decode"},
		{name: "metrics.counter_inc", manifest: metrics.ModuleTypes(), method: "counter_inc"},
		{name: "metrics.counter_add", manifest: metrics.ModuleTypes(), method: "counter_add"},
		{name: "metrics.gauge_set", manifest: metrics.ModuleTypes(), method: "gauge_set"},
		{name: "metrics.gauge_inc", manifest: metrics.ModuleTypes(), method: "gauge_inc"},
		{name: "metrics.gauge_dec", manifest: metrics.ModuleTypes(), method: "gauge_dec"},
		{name: "metrics.histogram", manifest: metrics.ModuleTypes(), method: "histogram"},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			iface := interfaceFromManifest(t, tc.manifest, tc.typeName)
			fn := lookupMethod(t, iface, tc.method)
			assertReturnsWithError(t, fn)
		})
	}
}

func TestCryptoSubmodules_ReturnsWithError(t *testing.T) {
	checks := []recordFieldCheck{
		{name: "crypto.random.bytes", manifest: crypto.ModuleTypes(), field: "random", method: "bytes"},
		{name: "crypto.random.string", manifest: crypto.ModuleTypes(), field: "random", method: "string"},
		{name: "crypto.random.uuid", manifest: crypto.ModuleTypes(), field: "random", method: "uuid"},
		{name: "crypto.hmac.sha256", manifest: crypto.ModuleTypes(), field: "hmac", method: "sha256"},
		{name: "crypto.hmac.sha512", manifest: crypto.ModuleTypes(), field: "hmac", method: "sha512"},
		{name: "crypto.encrypt.aes", manifest: crypto.ModuleTypes(), field: "encrypt", method: "aes"},
		{name: "crypto.encrypt.chacha20", manifest: crypto.ModuleTypes(), field: "encrypt", method: "chacha20"},
		{name: "crypto.decrypt.aes", manifest: crypto.ModuleTypes(), field: "decrypt", method: "aes"},
		{name: "crypto.decrypt.chacha20", manifest: crypto.ModuleTypes(), field: "decrypt", method: "chacha20"},
		{name: "crypto.jwt.encode", manifest: crypto.ModuleTypes(), field: "jwt", method: "encode"},
		{name: "crypto.jwt.verify", manifest: crypto.ModuleTypes(), field: "jwt", method: "verify"},
		{name: "crypto.pbkdf2", manifest: crypto.ModuleTypes(), field: "", method: "pbkdf2"},
	}

	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			var iface *typ.Interface
			if tc.field == "" {
				iface = interfaceFromManifest(t, tc.manifest, "")
			} else {
				iface = interfaceFromRecordField(t, tc.manifest, tc.field)
			}
			fn := lookupMethod(t, iface, tc.method)
			assertReturnsWithError(t, fn)
		})
	}
}

func interfaceFromManifest(t *testing.T, m *io.Manifest, typeName string) *typ.Interface {
	t.Helper()

	var target typ.Type
	if typeName == "" {
		target = m.Export
	} else {
		var ok bool
		target, ok = m.LookupType(typeName)
		if !ok {
			t.Fatalf("type %q not found in manifest", typeName)
		}
	}

	iface, ok := interfaceFromType(target)
	if !ok {
		t.Fatalf("type %q is not an interface or intersection with interface", typeName)
	}
	return iface
}

func interfaceFromRecordField(t *testing.T, m *io.Manifest, field string) *typ.Interface {
	t.Helper()

	record, ok := recordFromType(m.Export)
	if !ok {
		t.Fatalf("export type is not a record or intersection with record")
	}
	f := record.GetField(field)
	if f == nil {
		t.Fatalf("field %q not found on export record", field)
	}
	iface, ok := interfaceFromType(f.Type)
	if !ok {
		t.Fatalf("field %q is not an interface", field)
	}
	return iface
}

func interfaceFromType(t typ.Type) (*typ.Interface, bool) {
	switch tt := t.(type) {
	case *typ.Interface:
		return tt, true
	case *typ.Intersection:
		for _, m := range tt.Members {
			if iface, ok := m.(*typ.Interface); ok {
				return iface, true
			}
		}
	}
	return nil, false
}

func recordFromType(t typ.Type) (*typ.Record, bool) {
	switch tt := t.(type) {
	case *typ.Record:
		return tt, true
	case *typ.Intersection:
		for _, m := range tt.Members {
			if record, ok := m.(*typ.Record); ok {
				return record, true
			}
		}
	}
	return nil, false
}

func lookupMethod(t *testing.T, iface *typ.Interface, name string) *typ.Function {
	t.Helper()

	for _, method := range iface.Methods {
		if method.Name == name {
			return method.Type
		}
	}
	t.Fatalf("method %q not found", name)
	return nil
}

func assertReturnsWithError(t *testing.T, fn *typ.Function) {
	t.Helper()

	if len(fn.Returns) != 2 {
		t.Fatalf("expected 2 return values, got %d", len(fn.Returns))
	}
	if !typ.TypeEquals(fn.Returns[1], typ.NewOptional(typ.LuaError)) {
		t.Fatalf("expected second return to be optional LuaError, got %s", fn.Returns[1].String())
	}
}
