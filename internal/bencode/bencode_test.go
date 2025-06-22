package bencode

import (
	"bytes"
	"reflect"
	"testing"
)

func TestDecodeInt(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
		wantErr  bool
	}{
		{"i42e", 42, false},
		{"i0e", 0, false},
		{"i-42e", -42, false},
		{"i123456789e", 123456789, false},
		{"ixe", 0, true},  // Invalid integer
		{"i42", 0, true},  // Missing 'e'
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var result int64
			err := Decode([]byte(tt.input), &result)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if err == nil && result != tt.expected {
				t.Errorf("Decode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDecodeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
		wantErr  bool
	}{
		{"4:spam", "spam", false},
		{"0:", "", false},
		{"10:hello world", "hello worl", false}, // Only 10 chars
		{"5:hello", "hello", false},
		{"x:spam", "", true}, // Invalid length
		{"5:hi", "", true},   // String too short
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var result string
			err := Decode([]byte(tt.input), &result)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if err == nil && result != tt.expected {
				t.Errorf("Decode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDecodeList(t *testing.T) {
	tests := []struct {
		input    string
		expected []interface{}
		wantErr  bool
	}{
		{"le", []interface{}{}, false},
		{"li42e4:spame", []interface{}{int64(42), "spam"}, false},
		{"li1ei2ei3ee", []interface{}{int64(1), int64(2), int64(3)}, false},
		{"l4:spam4:eggse", []interface{}{"spam", "eggs"}, false},
		{"li42e", nil, true}, // Missing 'e'
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var result []interface{}
			err := Decode([]byte(tt.input), &result)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if err == nil && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Decode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestDecodeDict(t *testing.T) {
	tests := []struct {
		input    string
		expected map[string]interface{}
		wantErr  bool
	}{
		{"de", map[string]interface{}{}, false},
		{"d3:cow3:moo4:spam4:eggse", map[string]interface{}{"cow": "moo", "spam": "eggs"}, false},
		{"d3:fooi42ee", map[string]interface{}{"foo": int64(42)}, false},
		{"d4:spaml1:a1:bee", map[string]interface{}{"spam": []interface{}{"a", "b"}}, false},
		{"d3:foo", nil, true}, // Incomplete dict
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			var result map[string]interface{}
			err := Decode([]byte(tt.input), &result)
			
			if (err != nil) != tt.wantErr {
				t.Errorf("Decode() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			
			if err == nil && !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("Decode() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestEncodeInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{42, "i42e"},
		{0, "i0e"},
		{-42, "i-42e"},
		{123456789, "i123456789e"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result, err := Encode(tt.input)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			
			if string(result) != tt.expected {
				t.Errorf("Encode() = %v, want %v", string(result), tt.expected)
			}
		})
	}
}

func TestEncodeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"spam", "4:spam"},
		{"", "0:"},
		{"hello world", "11:hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := Encode(tt.input)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			
			if string(result) != tt.expected {
				t.Errorf("Encode() = %v, want %v", string(result), tt.expected)
			}
		})
	}
}

func TestEncodeList(t *testing.T) {
	tests := []struct {
		input    []interface{}
		expected string
	}{
		{[]interface{}{}, "le"},
		{[]interface{}{int64(42), "spam"}, "li42e4:spame"},
		{[]interface{}{int64(1), int64(2), int64(3)}, "li1ei2ei3ee"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result, err := Encode(tt.input)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			
			if string(result) != tt.expected {
				t.Errorf("Encode() = %v, want %v", string(result), tt.expected)
			}
		})
	}
}

func TestEncodeDict(t *testing.T) {
	tests := []struct {
		input    map[string]interface{}
		expected string
	}{
		{map[string]interface{}{}, "de"},
		{map[string]interface{}{"cow": "moo", "spam": "eggs"}, "d3:cow3:moo4:spam4:eggse"},
		{map[string]interface{}{"foo": int64(42)}, "d3:fooi42ee"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result, err := Encode(tt.input)
			if err != nil {
				t.Fatalf("Encode() error = %v", err)
			}
			
			if string(result) != tt.expected {
				t.Errorf("Encode() = %v, want %v", string(result), tt.expected)
			}
		})
	}
}

func TestStructEncoding(t *testing.T) {
	type TestStruct struct {
		Name  string `bencode:"name"`
		Age   int    `bencode:"age"`
		Email string `bencode:"email"`
	}

	input := TestStruct{
		Name:  "John",
		Age:   30,
		Email: "john@example.com",
	}

	// Encode
	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	// Decode back
	var decoded TestStruct
	err = Decode(encoded, &decoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !reflect.DeepEqual(input, decoded) {
		t.Errorf("Roundtrip failed: got %+v, want %+v", decoded, input)
	}
}

func TestByteSliceEncoding(t *testing.T) {
	input := []byte("hello world")
	
	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	
	expected := "11:hello world"
	if string(encoded) != expected {
		t.Errorf("Encode() = %v, want %v", string(encoded), expected)
	}
	
	var decoded []byte
	err = Decode(encoded, &decoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	
	if !bytes.Equal(input, decoded) {
		t.Errorf("Roundtrip failed: got %v, want %v", decoded, input)
	}
}

func TestComplexNesting(t *testing.T) {
	input := map[string]interface{}{
		"list": []interface{}{
			int64(1),
			"two",
			map[string]interface{}{
				"nested": "value",
			},
		},
		"number": int64(42),
		"string": "hello",
	}

	encoded, err := Encode(input)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var decoded map[string]interface{}
	err = Decode(encoded, &decoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !reflect.DeepEqual(input, decoded) {
		t.Errorf("Roundtrip failed: got %+v, want %+v", decoded, input)
	}
}