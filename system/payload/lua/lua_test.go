package lua

import (
	"fmt"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	lua "github.com/yuin/gopher-lua"
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

func (m *MockTranscoder) Unmarshal(p payload.Payload, v interface{}) error {
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
	golangPayload := payload.NewPayload(map[string]interface{}{"name": "Jane Doe", "age": 25}, payload.Golang)
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

	data, ok := golangPayload.Data().(map[string]interface{})
	if !ok {
		t.Fatalf("Expected map[string]interface{}, got %T", golangPayload.Data())
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
	Name           string          `lua:"name" json:"name"`
	Age            int             `json:"age"` // No Lua tag, should use case-insensitive matching
	Address        Address         `lua:"address" json:"address"`
	Hobbies        []string        `lua:"hobbies" json:"hobbies"`
	Roles          map[string]bool `lua:"roles" json:"roles"`
	IgnoredField   string          // No tag, should be ignored
	MissingField   string          `lua:"missing" json:"missing"`
	OptionalField  string          `json:"optional,omitempty"`
	NonNilPointer  *int            `lua:"nonNilPointer" json:"nonNilPointer"`
	NilPointer     *int            `lua:"nilPointer" json:"nilPointer"`
	InterfaceField interface{}     `lua:"interfaceField" json:"interfaceField"`
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
		if _, ok := p.InterfaceField.(map[string]interface{}); !ok {
			t.Errorf("Expected InterfaceField to be map[string]interface{}, got %T", p.InterfaceField)
		}
		// Assert the content of the interface field's value (if needed)
		innerMap, _ := p.InterfaceField.(map[string]interface{})
		if innerMap["innerKey"] != "innerValue" {
			t.Errorf("Expected InterfaceField.innerKey to be 'innerValue', got '%s'", innerMap["innerKey"])
		}
	}
}
