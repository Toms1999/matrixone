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

package index

import (
	"github.com/RoaringBitmap/roaring"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/compute"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/containers"
)

// A zonemap with 64-byte serialized data.
//
// If the data type is string, only a part of prefix of minimum and maximum will be written to disk
// Rule of thumb: false positive is allowed but false negative is not
// That means the searialized min-max range should cover the original min-max range.
//
// Therefore, we must record minv length, because filling zero for minv makes it bigger, which is not acceptable.
// For maxv, we have to construct a bigger value in 32 bytes by plus one if needed.
// What if the leading 32 bytes are all 0xff? That is means +inf, we should
// compare specifically, refer to the comments on isInf field
//
// Layout for string:
// [B0,...B30,B31,B32,...B62,B63]
//  ---------  -  --------------
//     minv    |       maxv
//             |
//         [b7=init,b6~b5 unused,b4~b0=len(minv)]

const (
	constZMInited uint8  = 0x80
	constMaxU64   uint64 = ^uint64(0)
)

func is32BytesMax(bs []byte) bool {
	isMax := true
	// iter u64 is about 8x faster than iter byte
	for i := 0; i < 32; i += 8 {
		if types.DecodeFixed[uint64](bs[i:i+8]) != constMaxU64 {
			isMax = false
			break
		}
	}
	return isMax
}

type ZoneMap struct {
	typ      types.Type
	min, max any
	inited   bool
	// only in a deserialized zonemap, this field is possibile to be True.
	// isInf is true means we can't find a 32-byte upper bound for original maximum when serializing,
	// and after deserializing, we have to infer that the original maximum is positive infinite.
	isInf bool
}

func NewZoneMap(typ types.Type) *ZoneMap {
	zm := &ZoneMap{typ: typ}
	return zm
}

func (zm *ZoneMap) GetType() types.Type {
	return zm.typ
}

func (zm *ZoneMap) init(v any) {
	zm.min = v
	zm.max = v
	zm.inited = true
}

func (zm *ZoneMap) Update(v any) (err error) {
	if !zm.inited {
		zm.init(v)
		return
	}
	if compute.CompareGeneric(v, zm.max, zm.typ) > 0 {
		zm.max = v
	} else if compute.CompareGeneric(v, zm.min, zm.typ) < 0 {
		zm.min = v
	}
	return
}

func (zm *ZoneMap) BatchUpdate(KeysCtx *KeysCtx) error {
	if !zm.typ.Eq(KeysCtx.Keys.GetType()) {
		return ErrWrongType
	}
	update := func(v any, _ int) error {
		return zm.Update(v)
	}
	if err := KeysCtx.Keys.ForeachWindow(KeysCtx.Start, KeysCtx.Count, update, nil); err != nil {
		return err
	}
	return nil
}

func (zm *ZoneMap) Contains(key any) (ok bool) {
	if !zm.inited {
		return
	}
	if (zm.isInf || compute.CompareGeneric(key, zm.max, zm.typ) <= 0) && compute.CompareGeneric(key, zm.min, zm.typ) >= 0 {
		ok = true
	}
	return
}

func (zm *ZoneMap) ContainsAny(keys containers.Vector) (visibility *roaring.Bitmap, ok bool) {
	if !zm.inited {
		return
	}
	visibility = roaring.NewBitmap()
	row := uint32(0)
	op := func(key any, _ int) (err error) {
		if (zm.isInf || compute.CompareGeneric(key, zm.max, zm.typ) <= 0) && compute.CompareGeneric(key, zm.min, zm.typ) >= 0 {
			visibility.Add(row)
		}
		row++
		return
	}
	if err := keys.Foreach(op, nil); err != nil {
		panic(err)
	}
	if visibility.GetCardinality() != 0 {
		ok = true
	}
	return
}

func (zm *ZoneMap) SetMax(v any) {
	if !zm.inited {
		zm.init(v)
		return
	}
	if compute.CompareGeneric(v, zm.max, zm.typ) > 0 {
		zm.max = v
	}
}

func (zm *ZoneMap) GetMax() any {
	return zm.max
}

func (zm *ZoneMap) SetMin(v any) {
	if !zm.inited {
		zm.init(v)
		return
	}
	if compute.CompareGeneric(v, zm.min, zm.typ) < 0 {
		zm.min = v
	}
}

func (zm *ZoneMap) GetMin() any {
	return zm.min
}

// func (zm *ZoneMap) Print() string {
// 	// default int32
// 	s := "<ZM>\n["
// 	s += strconv.Itoa(int(zm.min.(int32)))
// 	s += ","
// 	s += strconv.Itoa(int(zm.max.(int32)))
// 	s += "]\n"
// 	s += "</ZM>"
// 	return s
// }

func (zm *ZoneMap) Marshal() (buf []byte, err error) {
	buf = make([]byte, 64)
	if !zm.inited {
		return
	}
	buf[31] |= constZMInited
	switch zm.typ.Oid {
	case types.T_char, types.T_varchar, types.T_json:
		minv, maxv := zm.min.([]byte), zm.max.([]byte)
		// write 31-byte prefix of minv
		copy(buf[0:31], minv)
		minLen := uint8(31)
		if len(minv) < 31 {
			minLen = uint8(len(minv))
		}
		buf[31] |= minLen

		// write 32-byte prefix of maxv
		copy(buf[32:64], maxv)
		// no truncation, get a bigger value by filling tail zeros
		if len(maxv) > 32 && !is32BytesMax(buf[32:64]) {
			// truncation happens, get a bigger one by plus one
			for i := 63; i >= 32; i-- {
				buf[i] += 1
				if buf[i] != 0 {
					break
				}
			}
		}
	default:
		minv := types.EncodeValue(zm.min, zm.typ)
		maxv := types.EncodeValue(zm.max, zm.typ)
		if len(maxv) > 16 || len(minv) > 16 {
			panic("zonemap: large fixed length type, check again")
		}
		copy(buf[0:], minv)
		copy(buf[32:], maxv)
	}
	return
}

func (zm *ZoneMap) Unmarshal(buf []byte) error {
	init := buf[31] & constZMInited
	if init == 0 {
		zm.inited = false
		return nil
	}
	zm.inited = true
	switch zm.typ.Oid {
	case types.T_bool:
		zm.min = types.DecodeFixed[bool](buf[:1])
		buf = buf[32:]
		zm.max = types.DecodeFixed[bool](buf[:1])
		return nil
	case types.T_int8:
		zm.min = types.DecodeFixed[int8](buf[:1])
		buf = buf[32:]
		zm.max = types.DecodeFixed[int8](buf[:1])
		return nil
	case types.T_int16:
		zm.min = types.DecodeFixed[int16](buf[:2])
		buf = buf[32:]
		zm.max = types.DecodeFixed[int16](buf[:2])
		return nil
	case types.T_int32:
		zm.min = types.DecodeFixed[int32](buf[:4])
		buf = buf[32:]
		zm.max = types.DecodeFixed[int32](buf[:4])
		return nil
	case types.T_int64:
		zm.min = types.DecodeFixed[int64](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[int64](buf[:8])
		return nil
	case types.T_uint8:
		zm.min = types.DecodeFixed[uint8](buf[:1])
		buf = buf[32:]
		zm.max = types.DecodeFixed[uint8](buf[:1])
		return nil
	case types.T_uint16:
		zm.min = types.DecodeFixed[uint16](buf[:2])
		buf = buf[32:]
		zm.max = types.DecodeFixed[uint16](buf[:2])
		return nil
	case types.T_uint32:
		zm.min = types.DecodeFixed[uint32](buf[:4])
		buf = buf[32:]
		zm.max = types.DecodeFixed[uint32](buf[:4])
		return nil
	case types.T_uint64:
		zm.min = types.DecodeFixed[uint64](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[uint64](buf[:8])
		return nil
	case types.T_float32:
		zm.min = types.DecodeFixed[float32](buf[:4])
		buf = buf[32:]
		zm.max = types.DecodeFixed[float32](buf[:4])
		return nil
	case types.T_float64:
		zm.min = types.DecodeFixed[float64](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[float64](buf[:8])
		return nil
	case types.T_date:
		zm.min = types.DecodeFixed[types.Date](buf[:4])
		buf = buf[32:]
		zm.max = types.DecodeFixed[types.Date](buf[:4])
		return nil
	case types.T_datetime:
		zm.min = types.DecodeFixed[types.Datetime](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[types.Datetime](buf[:8])
		return nil
	case types.T_timestamp:
		zm.min = types.DecodeFixed[types.Timestamp](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[types.Timestamp](buf[:8])
		return nil
	case types.T_decimal64:
		zm.min = types.DecodeFixed[types.Decimal64](buf[:8])
		buf = buf[32:]
		zm.max = types.DecodeFixed[types.Decimal64](buf[:8])
		return nil
	case types.T_decimal128:
		zm.min = types.DecodeFixed[types.Decimal128](buf[:16])
		buf = buf[32:]
		zm.max = types.DecodeFixed[types.Decimal128](buf[:16])
		return nil
	case types.T_char, types.T_varchar, types.T_json:
		minBuf := make([]byte, buf[31]&0x7f)
		copy(minBuf, buf[0:32])
		maxBuf := make([]byte, 32)
		copy(maxBuf, buf[32:64])
		zm.min = minBuf
		zm.max = maxBuf

		zm.isInf = is32BytesMax(maxBuf)
		return nil
	default:
		panic("unsupported type")
	}
}

func (zm *ZoneMap) GetMemoryUsage() uint64 {
	return 64
}
