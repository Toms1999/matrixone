// Copyright 2021 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package types

import (
	"fmt"
	"strings"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"golang.org/x/exp/constraints"
)

type T uint8

const (
	// any family
	T_any  T = 0
	T_star T = 1

	// bool family
	T_bool T = 10

	// numeric/integer family
	T_int8    T = 20
	T_int16   T = 21
	T_int32   T = 22
	T_int64   T = 23
	T_int128  T = 24
	T_uint8   T = 25
	T_uint16  T = 26
	T_uint32  T = 27
	T_uint64  T = 28
	T_uint128 T = 29

	// numeric/float family
	T_float32 T = 30
	T_float64 T = 31

	// numeric/decimals
	T_decimal64  T = 32
	T_decimal128 T = 33

	// pseudo numerics, not used

	// date and time
	T_date      T = 50
	T_time      T = 51
	T_datetime  T = 52
	T_timestamp T = 53
	T_interval  T = 54

	// string family
	T_char    T = 60
	T_varchar T = 61
	T_json    T = 62

	// blobs
	T_blob T = 70

	// Transaction TS
	T_TS T = 100

	// system family
	T_sel   T = 200
	T_tuple T = 201
)

type Type struct {
	Oid  T     `json:"oid,string"`
	Size int32 `json:"size,string"` // e.g. int8.Size = 1, int16.Size = 2, char.Size = 24(SliceHeader size)

	// Width means max Display width for float and double, char and varchar // todo: need to add new attribute DisplayWidth ?
	Width int32 `json:"width,string"`

	Scale int32 `json:"Scale,string"`

	Precision int32 `json:"Precision,string"`
}

type Bytes struct {
	Data    []byte
	Offsets []uint32
	Lengths []uint32
}

type Date int32

type Datetime int64
type Timestamp int64

type Decimal64 [8]byte
type Decimal128 [16]byte

// timestamp for transaction: physical time (higher 8 bytes) + logical (lower 4 bytes)
// See txts.go for impl.
type TS [12]byte

type Ints interface {
	int8 | int16 | int32 | int64
}

type UInts interface {
	uint8 | uint16 | uint32 | uint64
}

type Floats interface {
	float32 | float64
}

type OrderedT interface {
	constraints.Ordered | Date | Datetime | Timestamp
}

type Decimal interface {
	Decimal64 | Decimal128
}

type FixedSizeT interface {
	bool | OrderedT | Decimal
}

type VarSizeT interface {
	Bytes
}

type Number interface {
	Ints | UInts | Floats | Decimal
}

type String interface {
	Get(int64) []byte
}

type Generic interface {
	Ints | UInts | Floats | Date | Datetime | Timestamp
}

var Types map[string]T = map[string]T{
	"bool": T_bool,

	"tinyint":  T_int8,
	"smallint": T_int16,
	"int":      T_int32,
	"integer":  T_int32,
	"bigint":   T_int64,

	"tinyint unsigned":  T_uint8,
	"smallint unsigned": T_uint16,
	"int unsigned":      T_uint32,
	"integer unsigned":  T_uint32,
	"bigint unsigned":   T_uint64,

	"decimal64":  T_decimal64,
	"decimal128": T_decimal128,

	"float":  T_float32,
	"double": T_float64,

	"date":      T_date,
	"datetime":  T_datetime,
	"timestamp": T_timestamp,
	"interval":  T_interval,

	"char":    T_char,
	"varchar": T_varchar,

	"json": T_json,
	"text": T_blob,
}

func New(oid T, width, scale, precision int32) Type {
	return Type{
		Oid:       oid,
		Width:     width,
		Scale:     scale,
		Precision: precision,
		Size:      int32(TypeSize(oid)),
	}
}

func TypeSize(oid T) int {
	return oid.TypeLen()
}

func (t Type) TypeSize() int {
	return t.Oid.TypeLen()
}

func (t Type) IsBoolean() bool {
	return t.Oid == T_bool
}

func (t Type) IsString() bool {
	return t.Oid == T_char || t.Oid == T_varchar || t.Oid == T_blob
}

func (t Type) IsIntOrUint() bool {
	switch t.Oid {
	case T_uint8, T_uint16, T_uint32, T_uint64, T_int8, T_int16, T_int32, T_int64:
		return true
	default:
		return false
	}
}

func (t Type) String() string {
	return t.Oid.String()
}

func (t Type) Eq(b Type) bool {
	return t.Oid == b.Oid && t.Size == b.Size && t.Width == b.Width && t.Scale == b.Scale
}

func (t T) ToType() Type {
	var typ Type

	typ.Oid = t
	switch t {
	case T_json:
		typ.Size = 24
	case T_bool:
		typ.Size = 1
	case T_int8:
		typ.Size = 1
	case T_int16:
		typ.Size = 2
	case T_int32, T_date:
		typ.Size = 4
	case T_int64, T_datetime, T_timestamp:
		typ.Size = 8
	case T_uint8:
		typ.Size = 1
	case T_uint16:
		typ.Size = 2
	case T_uint32:
		typ.Size = 4
	case T_uint64:
		typ.Size = 8
	case T_float32:
		typ.Size = 4
	case T_float64:
		typ.Size = 8
	case T_char:
		typ.Size = 24
	case T_varchar:
		typ.Size = 24
	case T_sel:
		typ.Size = 8
	case T_decimal64:
		typ.Size = 8
	case T_decimal128:
		typ.Size = 16
	case T_blob:
		typ.Size = 24
	}
	return typ
}

func (t T) String() string {
	switch t {
	case T_any:
		return "ANY"
	case T_bool:
		return "BOOL"
	case T_int8:
		return "TINYINT"
	case T_int16:
		return "SMALLINT"
	case T_int32:
		return "INT"
	case T_int64:
		return "BIGINT"
	case T_uint8:
		return "TINYINT UNSIGNED"
	case T_uint16:
		return "SMALLINT UNSIGNED"
	case T_uint32:
		return "INT UNSIGNED"
	case T_uint64:
		return "BIGINT UNSIGNED"
	case T_float32:
		return "FLOAT"
	case T_float64:
		return "DOUBLE"
	case T_date:
		return "DATE"
	case T_datetime:
		return "DATETIME"
	case T_timestamp:
		return "TIMESTAMP"
	case T_char:
		return "CHAR"
	case T_varchar:
		return "VARCHAR"
	case T_json:
		return "JSON"
	case T_sel:
		return "SEL"
	case T_tuple:
		return "TUPLE"
	case T_decimal64:
		return "DECIMAL64"
	case T_decimal128:
		return "DECIMAL128"
	case T_blob:
		return "TEXT"
	}
	return fmt.Sprintf("unexpected type: %d", t)
}

// functions only used to generate pkg/sql/colexec/extend/overload

// OidString returns T string
func (t T) OidString() string {
	switch t {
	case T_json:
		return "T_json"
	case T_bool:
		return "T_bool"
	case T_int64:
		return "T_int64"
	case T_int32:
		return "T_int32"
	case T_int16:
		return "T_int16"
	case T_int8:
		return "T_int8"
	case T_float64:
		return "T_float64"
	case T_float32:
		return "T_float32"
	case T_uint8:
		return "T_uint8"
	case T_uint16:
		return "T_uint16"
	case T_uint32:
		return "T_uint32"
	case T_uint64:
		return "T_uint64"
	case T_sel:
		return "T_sel"
	case T_char:
		return "T_char"
	case T_varchar:
		return "T_varchar"
	case T_date:
		return "T_date"
	case T_datetime:
		return "T_datetime"
	case T_timestamp:
		return "T_timestamp"
	case T_decimal64:
		return "T_decimal64"
	case T_decimal128:
		return "T_decimal128"
	case T_blob:
		return "T_blob"
	}
	return "unknown_type"
}

// GoType returns go type string for T
func (t T) GoType() string {
	switch t {
	case T_bool:
		return "bool"
	case T_int64:
		return "int64"
	case T_int32:
		return "int32"
	case T_int16:
		return "int16"
	case T_int8:
		return "int8"
	case T_float64:
		return "float64"
	case T_float32:
		return "float32"
	case T_uint8:
		return "uint8"
	case T_uint16:
		return "uint16"
	case T_uint32:
		return "uint32"
	case T_uint64:
		return "uint64"
	case T_sel:
		return "int64"
	case T_char:
		return "string"
	case T_varchar:
		return "string"
	case T_date:
		return "date"
	case T_datetime:
		return "datetime"
	case T_timestamp:
		return "timestamp"
	case T_decimal64:
		return "decimal64"
	case T_decimal128:
		return "decimal128"
	case T_blob:
		return "string"
	}
	return "unknown type"
}

// GoGoType returns special go type string for T
func (t T) GoGoType() string {
	if t == T_char || t == T_varchar || t == T_blob || t == T_json {
		return "Str"
	}
	k := t.GoType()
	return strings.ToUpper(k[:1]) + k[1:]
}

// TypeLen returns type's length whose type oid is T
func (t T) TypeLen() int {
	switch t {
	case T_json:
		return 24
	case T_int8, T_bool:
		return 1
	case T_int16:
		return 2
	case T_int32, T_date:
		return 4
	case T_int64, T_datetime, T_timestamp:
		return 8
	case T_uint8:
		return 1
	case T_uint16:
		return 2
	case T_uint32:
		return 4
	case T_uint64:
		return 8
	case T_float32:
		return 4
	case T_float64:
		return 8
	case T_char:
		return 24
	case T_varchar:
		return 24
	case T_sel:
		return 8
	case T_decimal64:
		return 8
	case T_decimal128:
		return 16
	case T_blob:
		return 24
	}
	panic(moerr.NewInternalError("Unknow type %s", t))
}

// FixedLength dangerous code, use TypeLen() if you don't want -8, -16, -24
func (t T) FixedLength() int {
	switch t {
	case T_json:
		return -24
	case T_int8, T_uint8, T_bool:
		return 1
	case T_int16, T_uint16:
		return 2
	case T_int32, T_uint32, T_date, T_float32:
		return 4
	case T_int64, T_uint64, T_datetime, T_float64, T_timestamp:
		return 8
	case T_decimal64:
		return -8
	case T_decimal128:
		return -16
	case T_char:
		return -24
	case T_varchar:
		return -24
	case T_sel:
		return 8
	case T_blob:
		return -24
	}
	panic(moerr.NewInternalError("Unknow type %s", t))
}

// isUnsignedInt: return true if the types.T is UnSigned integer type
func isUnsignedInt(t T) bool {
	if t == T_uint8 || t == T_uint16 || t == T_uint32 || t == T_uint64 {
		return true
	}
	return false
}

// isSignedInt: return true if the types.T is Signed integer type
func isSignedInt(t T) bool {
	if t == T_int8 || t == T_int16 || t == T_int32 || t == T_int64 {
		return true
	}
	return false
}

// if expr type is integer return true,else return false
func IsInteger(t T) bool {
	if isUnsignedInt(t) || isSignedInt(t) {
		return true
	}
	return false
}

// IsFloat: return true if the types.T is floating Point Types
func IsFloat(t T) bool {
	if t == T_float32 || t == T_float64 {
		return true
	}
	return false
}

// isString: return true if the types.T is string type
func IsString(t T) bool {
	if t == T_char || t == T_varchar || t == T_blob {
		return true
	}
	return false
}

func IsDateRelate(t T) bool {
	if t == T_date || t == T_datetime || t == T_timestamp {
		return true
	}
	return false
}

// IsDecimal: return true if the types.T is decimal64 or decimal128
func IsDecimal(t T) bool {
	if t == T_decimal64 || t == T_decimal128 {
		return true
	}
	return false
}
