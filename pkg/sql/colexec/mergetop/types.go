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

package mergetop

import (
	"github.com/matrixorigin/matrixone/pkg/compare"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/sql/colexec"
)

const (
	Build = iota
	Eval
	End
)

type container struct {
	n     int // result vector number
	state int
	sels  []int64
	poses []int32           // sorted list of attributes
	cmps  []compare.Compare // compare structure used to do sort work

	bat *batch.Batch // bat stores the final result of merge-top
}

type Argument struct {
	Limit int64           // Limit store the number of mergeTop-operator
	ctr   *container      // ctr stores the attributes needn't do Serialization work
	Fs    []colexec.Field // Fs store the order information
}

func (ctr *container) compare(vi, vj int, i, j int64) int {
	for _, pos := range ctr.poses {
		if r := ctr.cmps[pos].Compare(vi, vj, i, j); r != 0 {
			return r
		}
	}
	return 0
}

func (ctr *container) Len() int {
	return len(ctr.sels)
}

func (ctr *container) Less(i, j int) bool {
	return ctr.compare(0, 0, ctr.sels[i], ctr.sels[j]) > 0
}

func (ctr *container) Swap(i, j int) {
	ctr.sels[i], ctr.sels[j] = ctr.sels[j], ctr.sels[i]
}

func (ctr *container) Push(x interface{}) {
	ctr.sels = append(ctr.sels, x.(int64))
}

func (ctr *container) Pop() interface{} {
	n := len(ctr.sels) - 1
	x := ctr.sels[n]
	ctr.sels = ctr.sels[:n]
	return x
}
