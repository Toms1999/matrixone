// Copyright 2022 Matrix Origin
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

package binary

import (
	"github.com/matrixorigin/matrixone/pkg/container/nulls"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/vectorize/power"
	"github.com/matrixorigin/matrixone/pkg/vm/process"
)

func Power(vectors []*vector.Vector, proc *process.Process) (*vector.Vector, error) {
	left, right := vectors[0], vectors[1]
	resultType := types.Type{Oid: types.T_float64, Size: 8}
	resultElementSize := int(resultType.Size)
	leftValues, rightValues := vector.MustTCols[float64](left), vector.MustTCols[float64](right)
	switch {
	case left.IsScalar() && right.IsScalar():
		if left.ConstVectorIsNull() || right.ConstVectorIsNull() {
			return proc.AllocScalarNullVector(resultType), nil
		}
		resultVector := vector.NewConst(resultType, 1)
		resultValues := make([]float64, 1)
		vector.SetCol(resultVector, power.Power(leftValues, rightValues, resultValues))
		return resultVector, nil
	case left.IsScalar() && !right.IsScalar():
		if left.ConstVectorIsNull() {
			return proc.AllocScalarNullVector(resultType), nil
		}
		resultVector, err := proc.AllocVector(resultType, int64(resultElementSize*len(rightValues)))
		if err != nil {
			return nil, err
		}
		resultValues := types.DecodeFloat64Slice(resultVector.Data)
		resultValues = resultValues[:len(rightValues)]
		nulls.Set(resultVector.Nsp, right.Nsp)
		vector.SetCol(resultVector, power.PowerScalarLeftConst(leftValues[0], rightValues, resultValues))
		return resultVector, nil
	case !left.IsScalar() && right.IsScalar():
		if right.ConstVectorIsNull() {
			return proc.AllocScalarNullVector(resultType), nil
		}
		resultVector, err := proc.AllocVector(resultType, int64(resultElementSize*len(leftValues)))
		if err != nil {
			return nil, err
		}
		resultValues := types.DecodeFloat64Slice(resultVector.Data)
		resultValues = resultValues[:len(leftValues)]
		nulls.Set(resultVector.Nsp, left.Nsp)
		vector.SetCol(resultVector, power.PowerScalarRightConst(rightValues[0], leftValues, resultValues))
		return resultVector, nil
	}
	resultVector, err := proc.AllocVector(resultType, int64(resultElementSize*len(rightValues)))
	if err != nil {
		return nil, err
	}
	resultValues := types.DecodeFloat64Slice(resultVector.Data)
	resultValues = resultValues[:len(leftValues)]
	nulls.Or(left.Nsp, right.Nsp, resultVector.Nsp)
	vector.SetCol(resultVector, power.Power(leftValues, rightValues, resultValues))
	return resultVector, nil
}
