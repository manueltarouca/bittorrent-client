package bencode

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strconv"
)

var (
	ErrInvalidBencode = errors.New("invalid bencode")
	ErrUnexpectedEnd  = errors.New("unexpected end of bencode data")
)

type Decoder struct {
	r   io.Reader
	buf *bytes.Buffer
}

func NewDecoder(r io.Reader) *Decoder {
	return &Decoder{r: r}
}

func (d *Decoder) Decode(v interface{}) error {
	return d.decode(reflect.ValueOf(v))
}

func (d *Decoder) decode(v reflect.Value) error {
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			v.Set(reflect.New(v.Type().Elem()))
		}
		v = v.Elem()
	}

	b, err := d.readByte()
	if err != nil {
		return err
	}

	switch b {
	case 'i':
		return d.decodeInt(v)
	case 'l':
		return d.decodeList(v)
	case 'd':
		return d.decodeDict(v)
	case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		d.unreadByte(b)
		return d.decodeString(v)
	default:
		return fmt.Errorf("%w: unexpected byte %q", ErrInvalidBencode, b)
	}
}

func (d *Decoder) decodeInt(v reflect.Value) error {
	data, err := d.readUntil('e')
	if err != nil {
		return err
	}

	n, err := strconv.ParseInt(string(data), 10, 64)
	if err != nil {
		return fmt.Errorf("%w: invalid integer: %s", ErrInvalidBencode, err)
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(n)
	case reflect.Interface:
		v.Set(reflect.ValueOf(n))
	default:
		return fmt.Errorf("cannot decode integer into %v", v.Type())
	}

	return nil
}

func (d *Decoder) decodeString(v reflect.Value) error {
	lengthData, err := d.readUntil(':')
	if err != nil {
		return err
	}

	length, err := strconv.ParseInt(string(lengthData), 10, 64)
	if err != nil || length < 0 {
		return fmt.Errorf("%w: invalid string length", ErrInvalidBencode)
	}

	data := make([]byte, length)
	_, err = io.ReadFull(d.r, data)
	if err != nil {
		return err
	}

	switch v.Kind() {
	case reflect.String:
		v.SetString(string(data))
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes(data)
		} else {
			return fmt.Errorf("cannot decode string into %v", v.Type())
		}
	case reflect.Interface:
		v.Set(reflect.ValueOf(string(data)))
	default:
		return fmt.Errorf("cannot decode string into %v", v.Type())
	}

	return nil
}

func (d *Decoder) decodeList(v reflect.Value) error {
	var list []interface{}
	
	switch v.Kind() {
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
	case reflect.Interface:
		list = []interface{}{}
	default:
		return fmt.Errorf("cannot decode list into %v", v.Type())
	}

	for {
		b, err := d.peekByte()
		if err != nil {
			return err
		}
		if b == 'e' {
			d.readByte()
			break
		}

		if v.Kind() == reflect.Interface {
			var elem interface{}
			if err := d.decode(reflect.ValueOf(&elem).Elem()); err != nil {
				return err
			}
			list = append(list, elem)
		} else {
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.decode(elem); err != nil {
				return err
			}
			v.Set(reflect.Append(v, elem))
		}
	}

	if v.Kind() == reflect.Interface {
		v.Set(reflect.ValueOf(list))
	}

	return nil
}

func (d *Decoder) decodeDict(v reflect.Value) error {
	var m map[string]interface{}

	switch v.Kind() {
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return fmt.Errorf("map key must be string, got %v", v.Type().Key())
		}
		v.Set(reflect.MakeMap(v.Type()))
	case reflect.Struct:
		// For struct decoding, we'll use a temporary map
		m = make(map[string]interface{})
	case reflect.Interface:
		m = make(map[string]interface{})
		v.Set(reflect.ValueOf(m))
	default:
		return fmt.Errorf("cannot decode dict into %v", v.Type())
	}

	for {
		b, err := d.peekByte()
		if err != nil {
			return err
		}
		if b == 'e' {
			d.readByte()
			break
		}

		var key string
		if err := d.decode(reflect.ValueOf(&key).Elem()); err != nil {
			return err
		}

		if v.Kind() == reflect.Map {
			elem := reflect.New(v.Type().Elem()).Elem()
			if err := d.decode(elem); err != nil {
				return err
			}
			v.SetMapIndex(reflect.ValueOf(key), elem)
		} else if v.Kind() == reflect.Struct {
			var val interface{}
			if err := d.decode(reflect.ValueOf(&val).Elem()); err != nil {
				return err
			}
			m[key] = val
		} else {
			var val interface{}
			if err := d.decode(reflect.ValueOf(&val).Elem()); err != nil {
				return err
			}
			m[key] = val
		}
	}

	// If decoding into a struct, map the values
	if v.Kind() == reflect.Struct {
		return mapToStruct(m, v)
	}

	return nil
}

func mapToStruct(m map[string]interface{}, v reflect.Value) error {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := field.Tag.Get("bencode")
		if tag == "" {
			tag = field.Name
		}
		
		if val, ok := m[tag]; ok {
			fieldVal := v.Field(i)
			if !fieldVal.CanSet() {
				continue
			}
			
			// Convert the value to the appropriate type
			if err := setFieldValue(fieldVal, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func setFieldValue(field reflect.Value, val interface{}) error {
	valReflect := reflect.ValueOf(val)
	
	if field.Type() == valReflect.Type() {
		field.Set(valReflect)
		return nil
	}
	
	// Handle type conversions
	switch field.Kind() {
	case reflect.String:
		if s, ok := val.(string); ok {
			field.SetString(s)
			return nil
		}
	case reflect.Int, reflect.Int64:
		if i, ok := val.(int64); ok {
			field.SetInt(i)
			return nil
		}
	case reflect.Slice:
		if field.Type().Elem().Kind() == reflect.Uint8 {
			if s, ok := val.(string); ok {
				field.SetBytes([]byte(s))
				return nil
			}
		}
	}
	
	// Try to convert using reflection
	if valReflect.Type().ConvertibleTo(field.Type()) {
		field.Set(valReflect.Convert(field.Type()))
		return nil
	}
	
	return fmt.Errorf("cannot convert %T to %v", val, field.Type())
}

func (d *Decoder) readByte() (byte, error) {
	// Check buffer first
	if d.buf != nil && d.buf.Len() > 0 {
		return d.buf.ReadByte()
	}
	
	b := make([]byte, 1)
	_, err := io.ReadFull(d.r, b)
	return b[0], err
}

func (d *Decoder) peekByte() (byte, error) {
	b, err := d.readByte()
	if err != nil {
		return 0, err
	}
	d.unreadByte(b)
	return b, nil
}

func (d *Decoder) unreadByte(b byte) {
	// Store the byte in a buffer if we need to unread
	if d.buf == nil {
		d.buf = &bytes.Buffer{}
	}
	d.buf.WriteByte(b)
}

func (d *Decoder) readUntil(delim byte) ([]byte, error) {
	var buf []byte
	for {
		b, err := d.readByte()
		if err != nil {
			return nil, err
		}
		if b == delim {
			return buf, nil
		}
		buf = append(buf, b)
	}
}

// Encode functions

type Encoder struct {
	w io.Writer
}

func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

func (e *Encoder) Encode(v interface{}) error {
	return e.encode(reflect.ValueOf(v))
}

func (e *Encoder) encode(v reflect.Value) error {
	// Handle pointers and interfaces
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return errors.New("cannot encode nil")
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return e.encodeInt(v.Int())
	case reflect.String:
		return e.encodeString(v.String())
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return e.encodeBytes(v.Bytes())
		}
		return e.encodeList(v)
	case reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return e.encodeBytes(v.Slice(0, v.Len()).Bytes())
		}
		return e.encodeList(v)
	case reflect.Map:
		return e.encodeDict(v)
	case reflect.Struct:
		return e.encodeStruct(v)
	default:
		return fmt.Errorf("unsupported type: %v", v.Type())
	}
}

func (e *Encoder) encodeInt(n int64) error {
	_, err := fmt.Fprintf(e.w, "i%de", n)
	return err
}

func (e *Encoder) encodeString(s string) error {
	return e.encodeBytes([]byte(s))
}

func (e *Encoder) encodeBytes(b []byte) error {
	_, err := fmt.Fprintf(e.w, "%d:", len(b))
	if err != nil {
		return err
	}
	_, err = e.w.Write(b)
	return err
}

func (e *Encoder) encodeList(v reflect.Value) error {
	if _, err := e.w.Write([]byte{'l'}); err != nil {
		return err
	}

	for i := 0; i < v.Len(); i++ {
		if err := e.encode(v.Index(i)); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte{'e'})
	return err
}

func (e *Encoder) encodeDict(v reflect.Value) error {
	if v.Type().Key().Kind() != reflect.String {
		return errors.New("dict key must be string")
	}

	if _, err := e.w.Write([]byte{'d'}); err != nil {
		return err
	}

	// Sort keys for consistent encoding
	keys := v.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})

	for _, key := range keys {
		if err := e.encodeString(key.String()); err != nil {
			return err
		}
		if err := e.encode(v.MapIndex(key)); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte{'e'})
	return err
}

func (e *Encoder) encodeStruct(v reflect.Value) error {
	if _, err := e.w.Write([]byte{'d'}); err != nil {
		return err
	}

	// Create a map of fields to encode
	fields := make(map[string]reflect.Value)
	t := v.Type()
	
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fieldValue := v.Field(i)
		
		// Skip unexported fields
		if !fieldValue.CanInterface() {
			continue
		}
		
		// Get the field name from bencode tag or use field name
		tag := field.Tag.Get("bencode")
		if tag == "" {
			tag = field.Name
		} else if tag == "-" {
			continue
		}
		
		fields[tag] = fieldValue
	}
	
	// Sort keys for consistent encoding
	var keys []string
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	
	// Encode fields
	for _, key := range keys {
		if err := e.encodeString(key); err != nil {
			return err
		}
		if err := e.encode(fields[key]); err != nil {
			return err
		}
	}

	_, err := e.w.Write([]byte{'e'})
	return err
}

// Convenience functions

func Decode(data []byte, v interface{}) error {
	return NewDecoder(bytes.NewReader(data)).Decode(v)
}

func Encode(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := NewEncoder(&buf).Encode(v)
	return buf.Bytes(), err
}