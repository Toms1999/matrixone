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

package stddevpop

import (
	"testing"

	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/sql/colexec/agg"
	"github.com/matrixorigin/matrixone/pkg/testutil"
	"github.com/matrixorigin/matrixone/pkg/vm/mheap"
	"github.com/matrixorigin/matrixone/pkg/vm/mmu/guest"
	"github.com/matrixorigin/matrixone/pkg/vm/mmu/host"
	"github.com/stretchr/testify/require"
)

const (
	Rows = 10
)

func TestStddevpop(t *testing.T) {
	inputType := types.New(types.T_int8, 0, 0, 0)
	sdp1 := New[int8]()
	sdp2 := New[int8]()
	sdp3 := New[int8]()
	m := mheap.New(guest.New(1<<30, host.New(1<<30)))
	vec := testutil.NewVector(Rows, inputType, m, false, nil)
	{
		agg := agg.NewUnaryAgg(sdp1, true, inputType, types.New(types.T_float64, 0, 0, 0), sdp1.Grows, sdp1.Eval, sdp1.Merge, sdp1.Fill, nil)
		err := agg.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		v, err := agg.Eval(m)
		require.NoError(t, err)
		require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
		v.Free(m)
	}
	{
		agg0 := agg.NewUnaryAgg(sdp2, true, inputType, types.New(types.T_float64, 0, 0, 0), sdp2.Grows, sdp2.Eval, sdp2.Merge, sdp2.Fill, nil)
		err := agg0.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg0.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg1 := agg.NewUnaryAgg(sdp3, true, inputType, types.New(types.T_float64, 0, 0, 0), sdp3.Grows, sdp3.Eval, sdp3.Merge, sdp3.Fill, nil)
		err = agg1.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg1.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg0.Merge(agg1, 0, 0)
		{
			v, err := agg0.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
		{
			v, err := agg1.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
	}
	vec.Free(m)
	require.Equal(t, int64(0), m.Size())
}

func TestDist(t *testing.T) {
	inputType := types.New(types.T_int8, 0, 0, 0)
	sdp1 := New[int8]()
	sdp2 := New[int8]()
	sdp3 := New[int8]()
	m := mheap.New(guest.New(1<<30, host.New(1<<30)))
	vec := testutil.NewVector(Rows, inputType, m, false, nil)
	{
		agg := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp1.Grows, sdp1.Eval, sdp1.Merge, sdp1.Fill)
		err := agg.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		v, err := agg.Eval(m)
		require.NoError(t, err)
		require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
		v.Free(m)
	}
	{
		agg0 := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp2.Grows, sdp2.Eval, sdp2.Merge, sdp2.Fill)
		err := agg0.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg0.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg1 := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp3.Grows, sdp3.Eval, sdp3.Merge, sdp3.Fill)
		err = agg1.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg1.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg0.Merge(agg1, 0, 0)
		{
			v, err := agg0.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
		{
			v, err := agg1.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
	}
	vec.Free(m)
	require.Equal(t, int64(0), m.Size())
}

func TestDecimalDist(t *testing.T) {
	inputType := types.New(types.T_decimal64, 0, 0, 0)
	sdp1 := New2()
	sdp2 := New2()
	sdp3 := New2()
	m := mheap.New(guest.New(1<<30, host.New(1<<30)))
	vec := testutil.NewVector(Rows, inputType, m, false, nil)
	{
		agg := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp1.Grows, sdp1.Eval, sdp1.Merge, sdp1.Fill)
		err := agg.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		v, err := agg.Eval(m)
		require.NoError(t, err)
		require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
		v.Free(m)
	}
	{
		agg0 := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp2.Grows, sdp2.Eval, sdp2.Merge, sdp2.Fill)
		err := agg0.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg0.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg1 := agg.NewUnaryDistAgg(true, inputType, types.New(types.T_float64, 0, 0, 0), sdp3.Grows, sdp3.Eval, sdp3.Merge, sdp3.Fill)
		err = agg1.Grows(1, m)
		require.NoError(t, err)
		for i := 0; i < Rows; i++ {
			agg1.Fill(0, int64(i), 1, []*vector.Vector{vec})
		}
		agg0.Merge(agg1, 0, 0)
		{
			v, err := agg0.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
		{
			v, err := agg1.Eval(m)
			require.NoError(t, err)
			require.Equal(t, []float64{2.8722813232690143}, vector.GetColumn[float64](v))
			v.Free(m)
		}
	}
	vec.Free(m)
	require.Equal(t, int64(0), m.Size())
}
