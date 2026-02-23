// SPDX-License-Identifier: MPL-2.0

package payload

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/topology"
	systempayload "github.com/wippyai/runtime/system/payload"
)

// MockTranscoder is a mock implementation of payload.Transcoder for testing.
type MockTranscoder struct {
	registeredTranscoders  map[payload.Format]map[payload.Format]payload.FormatTranscoder
	registeredUnmarshalers map[payload.Format]payload.Unmarshaler
}

func NewMockTranscoder() *MockTranscoder {
	return &MockTranscoder{
		registeredTranscoders:  make(map[payload.Format]map[payload.Format]payload.FormatTranscoder),
		registeredUnmarshalers: make(map[payload.Format]payload.Unmarshaler),
	}
}

func (m *MockTranscoder) RegisterTranscoder(from, to payload.Format, _ int, tt payload.FormatTranscoder) {
	if _, ok := m.registeredTranscoders[from]; !ok {
		m.registeredTranscoders[from] = make(map[payload.Format]payload.FormatTranscoder)
	}
	m.registeredTranscoders[from][to] = tt
}

func (m *MockTranscoder) RegisterUnmarshaler(from payload.Format, unmarshaler payload.Unmarshaler) {
	m.registeredUnmarshalers[from] = unmarshaler
}

func (m *MockTranscoder) Transcode(p payload.Payload, to payload.Format) (payload.Payload, error) {
	if transcoders, ok := m.registeredTranscoders[p.Format()]; ok {
		if transcoder, ok := transcoders[to]; ok {
			return transcoder.Transcode(p)
		}
	}
	return nil, fmt.Errorf("no transcoder found for %s to %s", p.Format(), to)
}

func (m *MockTranscoder) Unmarshal(p payload.Payload, v any) error {
	if unmarshaler, ok := m.registeredUnmarshalers[p.Format()]; ok {
		return unmarshaler.Unmarshal(p, v)
	}
	return fmt.Errorf("no unmarshaler found for %s", p.Format())
}

func TestLuaTranscodersAndUnmarshaler(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	Register(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	// Example Lua table
	tbl := l.NewTable()
	l.SetTable(tbl, lua.LString("name"), lua.LString("John Doe"))
	l.SetTable(tbl, lua.LString("age"), lua.LNumber(30))

	// Test Golang -> Lua
	golangPayload := payload.NewPayload(map[string]any{"name": "Jane Doe", "age": 25}, payload.Golang)
	luaPayload, err := mockTranscoder.Transcode(golangPayload, payload.Lua)
	if err != nil {
		t.Fatalf("Error transcoding to Lua: %v", err)
	}

	if luaPayload.Format() != payload.Lua {
		t.Errorf("Expected Lua format, got %s", luaPayload.Format())
	}

	// Test Lua -> Golang
	originalLuaPayload := payload.NewPayload(tbl, payload.Lua)
	golangPayload, err = mockTranscoder.Transcode(originalLuaPayload, payload.Golang)
	if err != nil {
		t.Fatalf("Error transcoding to Golang: %v", err)
	}

	if golangPayload.Format() != payload.Golang {
		t.Errorf("Expected Golang format, got %s", golangPayload.Format())
	}

	data, ok := golangPayload.Data().(map[string]any)
	if !ok {
		t.Fatalf("Expected map[string]any, got %T", golangPayload.Data())
	}

	if data["name"] != "John Doe" || int(data["age"].(float64)) != 30 {
		t.Errorf("Unexpected data: %v", data)
	}

	// Test Unmarshal
	type Person struct {
		Name string `lua:"name"`
		Age  int    `lua:"age"`
	}
	var p Person
	err = mockTranscoder.Unmarshal(originalLuaPayload, &p)
	if err != nil {
		t.Fatalf("Error unmarshalling: %v", err)
	}

	if p.Name != "John Doe" || p.Age != 30 {
		t.Errorf("Unexpected person data: %v", p)
	}
}

type Address struct {
	Street string `lua:"street" json:"street"`
	City   string `lua:"city" json:"city"`
}

type Person struct {
	InterfaceField any             `lua:"interfaceField" json:"interfaceField"`
	Roles          map[string]bool `lua:"roles" json:"roles"`
	NonNilPointer  *int            `lua:"nonNilPointer" json:"nonNilPointer"`
	NilPointer     *int            `lua:"nilPointer" json:"nilPointer"`
	Address        Address         `lua:"address" json:"address"`
	Name           string          `lua:"name" json:"name"`
	IgnoredField   string
	MissingField   string   `lua:"missing" json:"missing"`
	OptionalField  string   `json:"optional,omitempty"`
	Hobbies        []string `lua:"hobbies" json:"hobbies"`
	Age            int      `json:"age"`
}

type SpecialPerson struct {
	Name string `json:"personName,omitempty"` // Using json tag to map to "personName" in Lua
	Age  int    `json:"age"`
}

func TestLuaUnmarshalerRecursive(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	Register(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	// Example Lua data
	luaData := `
	person = {
		name = "John Doe",
		age = 30,
		address = {
			street = "123 Main St",
			city = "Anytown"
		},
		hobbies = {"reading", "hiking", "coding"},
		roles = {
			admin = true,
			user = false
		},
		personName = "Jane Doe",
		interfaceField = {
			innerKey = "innerValue"
		},
		nonNilPointer = 10
	}
	`

	err := l.DoString(luaData)
	if err != nil {
		t.Fatalf("Error loading Lua data: %v", err)
	}

	tbl := l.GetGlobal("person")
	originalLuaPayload := payload.NewPayload(tbl, payload.Lua)

	// Test with a struct that has lua tags
	var p Person
	// Test with a struct that uses json tags for some fields
	var sp SpecialPerson
	// Initialize the NonNilPointer field to test non-nil pointers
	initialValue := 10
	p.NonNilPointer = &initialValue

	err = mockTranscoder.Unmarshal(originalLuaPayload, &p)
	if err != nil {
		t.Fatalf("Error unmarshalling to Person: %v", err)
	}

	err = mockTranscoder.Unmarshal(originalLuaPayload, &sp)
	if err != nil {
		t.Fatalf("Error unmarshalling to SpecialPerson: %v", err)
	}

	// Assertions for Person
	if p.Name != "John Doe" {
		t.Errorf("Expected name 'John Doe', got '%s'", p.Name)
	}
	if p.Age != 30 {
		t.Errorf("Expected age 30, got %d", p.Age)
	}
	if p.Address.Street != "123 Main St" {
		t.Errorf("Expected address street '123 Main St', got '%s'", p.Address.Street)
	}
	if p.Address.City != "Anytown" {
		t.Errorf("Expected address city 'Anytown', got '%s'", p.Address.City)
	}
	if len(p.Hobbies) != 3 || p.Hobbies[0] != "reading" || p.Hobbies[1] != "hiking" || p.Hobbies[2] != "coding" {
		t.Errorf("Expected hobbies ['reading', 'hiking', 'coding'], got %v", p.Hobbies)
	}
	if p.Roles["admin"] != true || p.Roles["user"] != false {
		t.Errorf("Expected roles {admin=true, user=false}, got %v", p.Roles)
	}
	if p.IgnoredField != "" {
		t.Errorf("Expected IgnoredField to be ignored and empty, got '%s'", p.IgnoredField)
	}
	if p.MissingField != "" {
		t.Errorf("Expected MissingField to be empty (no matching key), got '%s'", p.MissingField)
	}
	if p.OptionalField != "" {
		t.Errorf("Expected OptionalField to be empty (omitempty), got '%s'", p.OptionalField)
	}
	// Assertions for SpecialPerson (using json tag)
	if sp.Name != "Jane Doe" {
		t.Errorf("Expected special person name 'Jane Doe', got '%s'", sp.Name)
	}
	if sp.Age != 30 {
		t.Errorf("Expected special person age 30, got %d", sp.Age)
	}
	if *p.NonNilPointer != 10 {
		t.Errorf("Expected NonNilPointer to be 10, got %d", *p.NonNilPointer)
	}
	if p.NilPointer != nil {
		t.Errorf("Expected NilPointer to be nil, got %v", *p.NilPointer)
	}
	if p.InterfaceField == nil {
		t.Errorf("Expected InterfaceField to be not nil")
	} else {
		// Assert the type of the interface field's value
		if _, ok := p.InterfaceField.(map[string]any); !ok {
			t.Errorf("Expected InterfaceField to be map[string]any, got %T", p.InterfaceField)
		}
		// Assert the content of the interface field's value (if needed)
		innerMap, _ := p.InterfaceField.(map[string]any)
		if innerMap["innerKey"] != "innerValue" {
			t.Errorf("Expected InterfaceField.innerKey to be 'innerValue', got '%s'", innerMap["innerKey"])
		}
	}
}

func TestLuaTranscoderErrorCases(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	Register(mockTranscoder)

	// Test invalid format for ToGolang
	invalidPayload := payload.NewPayload("test", payload.JSON)
	_, err := mockTranscoder.Transcode(invalidPayload, payload.Golang)
	if err == nil {
		t.Error("Expected error when transcoding from non-Lua format to Golang")
	}

	// Test invalid format for FromGolang
	_, err = mockTranscoder.Transcode(invalidPayload, payload.Lua)
	if err == nil {
		t.Error("Expected error when transcoding from non-Golang format to Lua")
	}

	// Test invalid data type for ToGolang
	l := lua.NewState()
	defer l.Close()
	invalidLuaPayload := payload.NewPayload("not a lua value", payload.Lua)
	_, err = mockTranscoder.Transcode(invalidLuaPayload, payload.Golang)
	if err == nil {
		t.Error("Expected error when transcoding invalid Lua value to Golang")
	}
}

func TestLuaTranscoderNilAndEmptyValues(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	Register(mockTranscoder)

	l := lua.NewState()
	defer l.Close()

	// Test nil value
	nilPayload := payload.NewPayload(lua.LNil, payload.Lua)
	golangPayload, err := mockTranscoder.Transcode(nilPayload, payload.Golang)
	if err != nil {
		t.Fatalf("Error transcoding nil value: %v", err)
	}
	if golangPayload.Data() != nil {
		t.Errorf("Expected nil value, got %v", golangPayload.Data())
	}

	// Test empty table
	emptyTable := l.NewTable()
	emptyTablePayload := payload.NewPayload(emptyTable, payload.Lua)
	golangPayload, err = mockTranscoder.Transcode(emptyTablePayload, payload.Golang)
	if err != nil {
		t.Fatalf("Error transcoding empty table: %v", err)
	}
	data, ok := golangPayload.Data().(map[string]any)
	if !ok {
		t.Fatalf("Expected map[string]any, got %T", golangPayload.Data())
	}
	if len(data) != 0 {
		t.Errorf("Expected empty map, got %v", data)
	}

	// Test nil pointer in struct
	type TestStruct struct {
		Ptr *int `lua:"ptr"`
	}

	// Create a Lua table with the expected structure
	nilPtrTable := l.NewTable()
	l.SetTable(nilPtrTable, lua.LString("ptr"), lua.LNil)
	l.SetTable(nilPtrTable, lua.LString("_type"), lua.LString("object")) // Add a type marker to ensure it's treated as an object

	// Debug: Print the table contents
	t.Logf("Lua table contents: %v", nilPtrTable)

	nilPtrPayload := payload.NewPayload(nilPtrTable, payload.Lua)

	// First test transcoding to Golang
	golangPayload, err = mockTranscoder.Transcode(nilPtrPayload, payload.Golang)
	if err != nil {
		t.Fatalf("Error transcoding to Golang: %v", err)
	}

	// Debug: Print the Golang payload
	t.Logf("Golang payload: %v", golangPayload.Data())

	// First try to unmarshal to a map to verify the structure
	var mapData map[string]any
	err = mockTranscoder.Unmarshal(nilPtrPayload, &mapData)
	if err != nil {
		t.Fatalf("Error unmarshalling to map: %v", err)
	}

	// Now try to unmarshal to the struct
	var ts TestStruct
	err = mockTranscoder.Unmarshal(nilPtrPayload, &ts)
	if err != nil {
		t.Fatalf("Error unmarshalling to struct with nil pointer: %v", err)
	}
	if ts.Ptr != nil {
		t.Errorf("Expected nil pointer, got %v", ts.Ptr)
	}
}

func TestLuaTranscoderRegistration(t *testing.T) {
	mockTranscoder := NewMockTranscoder()
	Register(mockTranscoder)

	// Verify all transcoders are registered
	if len(mockTranscoder.registeredTranscoders) == 0 {
		t.Error("No transcoders registered")
	}

	// Verify Lua to Golang transcoder
	if _, ok := mockTranscoder.registeredTranscoders[payload.Lua][payload.Golang]; !ok {
		t.Error("Lua to Golang transcoder not registered")
	}

	// Verify Golang to Lua transcoder
	if _, ok := mockTranscoder.registeredTranscoders[payload.Golang][payload.Lua]; !ok {
		t.Error("Golang to Lua transcoder not registered")
	}

	// Verify unmarshaler
	if _, ok := mockTranscoder.registeredUnmarshalers[payload.Lua]; !ok {
		t.Error("Lua unmarshaler not registered")
	}
}

func TestFromGolang_NestedPayloadUsesParentTranscoder(t *testing.T) {
	dtt := systempayload.NewTranscoder()
	Register(dtt)

	input := map[string]any{
		"result": payload.NewPayload([]byte(`{"user_id":"u1","count":2}`), payload.JSON),
	}

	out, err := dtt.Transcode(payload.NewPayload(input, payload.Golang), payload.Lua)
	if err != nil {
		t.Fatalf("transcode failed: %v", err)
	}

	root, ok := out.Data().(*lua.LTable)
	if !ok {
		t.Fatalf("expected Lua table, got %T", out.Data())
	}

	resultVal := root.RawGetString("result")
	resultTbl, ok := resultVal.(*lua.LTable)
	if !ok {
		t.Fatalf("expected result to be Lua table, got %T (%v)", resultVal, resultVal)
	}

	if got := resultTbl.RawGetString("user_id").String(); got != "u1" {
		t.Fatalf("expected user_id=u1, got %s", got)
	}
	if got := resultTbl.RawGetString("count"); got.String() != "2" {
		t.Fatalf("expected count=2, got %v", got)
	}
}

func TestFromGolang_PreservesSpecialTypesInStructs(t *testing.T) {
	dtt := systempayload.NewTranscoder()
	Register(dtt)

	type eventLike struct {
		At   time.Time     `json:"at"`
		Err  error         `json:"err"`
		From pid.PID       `json:"from"`
		Wait time.Duration `json:"wait"`
	}

	at := time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC)
	wait := 3*time.Second + 250*time.Millisecond
	input := eventLike{
		At:   at,
		Wait: wait,
		From: pid.PID{Node: "node1", Host: "app:processes", UniqID: "abc123"},
		Err:  errors.New("boom"),
	}

	out, err := dtt.Transcode(payload.NewPayload(input, payload.Golang), payload.Lua)
	if err != nil {
		t.Fatalf("transcode failed: %v", err)
	}

	root, ok := out.Data().(*lua.LTable)
	if !ok {
		t.Fatalf("expected Lua table, got %T", out.Data())
	}

	fromVal := root.RawGetString("from")
	if fromVal.Type() != lua.LTString {
		t.Fatalf("expected from to be string, got %s (%T)", fromVal.Type(), fromVal)
	}
	if got := fromVal.String(); got != "{node1@app:processes|abc123}" {
		t.Fatalf("expected canonical pid string, got %s", got)
	}

	atVal := root.RawGetString("at")
	if atVal.Type() != lua.LTNumber && atVal.Type() != lua.LTInteger {
		t.Fatalf("expected at to be number/integer, got %s (%v)", atVal.Type(), atVal)
	}
	if got := atVal.String(); got != fmt.Sprintf("%d", at.Unix()) {
		t.Fatalf("expected unix seconds %d, got %s", at.Unix(), got)
	}

	waitVal := root.RawGetString("wait")
	if waitVal.Type() != lua.LTInteger {
		t.Fatalf("expected wait to be integer, got %s (%v)", waitVal.Type(), waitVal)
	}
	if got := waitVal.String(); got != fmt.Sprintf("%d", wait.Nanoseconds()) {
		t.Fatalf("expected duration nanoseconds %d, got %s", wait.Nanoseconds(), got)
	}

	errVal := root.RawGetString("err")
	if errVal == lua.LNil {
		t.Fatal("expected err field to be non-nil")
	}
	luaErr, ok := errVal.(*lua.Error)
	if !ok {
		t.Fatalf("expected err to be *lua.Error, got %T (%v)", errVal, errVal)
	}
	if got := luaErr.Error(); !strings.Contains(got, "boom") {
		t.Fatalf("expected err to contain boom, got %s", got)
	}
}

func TestFromGolang_TemporalExitEvent_NestedPayloads(t *testing.T) {
	dtt := systempayload.NewTranscoder()
	Register(dtt)

	jsonResult := payload.NewPayload([]byte(`{"received":"hello from parent","status":"child done"}`), payload.JSON)
	event := &topology.ExitEvent{
		At:   time.Date(2026, 2, 5, 12, 0, 0, 0, time.UTC),
		Kind: topology.Exit,
		From: pid.PID{Node: "node1", Host: "test-queue", UniqID: "child-1"},
		Result: &runtimeapi.Result{
			Value: jsonResult,
		},
	}

	out, err := dtt.Transcode(payload.NewPayload(event, payload.Golang), payload.Lua)
	if err != nil {
		t.Fatalf("transcode failed: %v", err)
	}

	root, ok := out.Data().(*lua.LTable)
	if !ok {
		t.Fatalf("expected Lua table, got %T", out.Data())
	}

	fromVal := root.RawGetString("from")
	if fromVal.Type() != lua.LTString {
		t.Fatalf("expected from string, got %s (%T)", fromVal.Type(), fromVal)
	}
	if got := fromVal.String(); got != "{node1@test-queue|child-1}" {
		t.Fatalf("expected canonical pid string, got %s", got)
	}

	resultTbl, ok := root.RawGetString("result").(*lua.LTable)
	if !ok {
		t.Fatalf("expected result table, got %T", root.RawGetString("result"))
	}
	valueTbl, ok := resultTbl.RawGetString("value").(*lua.LTable)
	if !ok {
		t.Fatalf("expected result.value table, got %T (%v)", resultTbl.RawGetString("value"), resultTbl.RawGetString("value"))
	}
	if got := valueTbl.RawGetString("received").String(); got != "hello from parent" {
		t.Fatalf("expected received field to decode as JSON table, got %s", got)
	}
	if got := valueTbl.RawGetString("status").String(); got != "child done" {
		t.Fatalf("expected status field to decode as JSON table, got %s", got)
	}

	// Bytes payload should remain Lua string bytes, not stringified Go slice.
	event.Result.Value = payload.NewPayload([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, payload.Bytes)
	out, err = dtt.Transcode(payload.NewPayload(event, payload.Golang), payload.Lua)
	if err != nil {
		t.Fatalf("transcode failed for bytes payload: %v", err)
	}

	root = out.Data().(*lua.LTable)
	resultTbl = root.RawGetString("result").(*lua.LTable)
	bytesVal := resultTbl.RawGetString("value")
	if bytesVal.Type() != lua.LTString {
		t.Fatalf("expected result.value bytes to be Lua string, got %s (%T)", bytesVal.Type(), bytesVal)
	}
	if len([]byte(bytesVal.String())) != 16 {
		t.Fatalf("expected 16 byte Lua string, got %d", len([]byte(bytesVal.String())))
	}
}
