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

package updates

import (
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"testing"
	"time"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/testutils"
	"github.com/stretchr/testify/assert"
)

func TestMutationControllerAppend(t *testing.T) {
	testutils.EnsureNoLeak(t)
	mc := NewMVCCHandle(nil)

	nodeCnt := 10000
	rowsPerNode := uint32(5)
	//ts := uint64(2)
	//ts = 4
	ts := types.NextGlobalTsForTest().Next().Next()
	//queries := make([]uint64, 0)
	//queries = append(queries, ts-1)
	queries := make([]types.TS, 0)
	queries = append(queries, ts.Prev())

	for i := 0; i < nodeCnt; i++ {
		txn := mockTxn()
		txn.CommitTS = ts
		node, _ := mc.AddAppendNodeLocked(txn, rowsPerNode*uint32(i), rowsPerNode*(uint32(i)+1))
		err := node.ApplyCommit(nil)
		assert.Nil(t, err)
		//queries = append(queries, ts+1)
		queries = append(queries, ts.Next())
		//ts += 2
		ts = ts.Next().Next()
	}

	st := time.Now()
	for i, qts := range queries {
		row, ok, _ := mc.GetMaxVisibleRowLocked(qts)
		if i == 0 {
			assert.False(t, ok)
		} else {
			assert.True(t, ok)
			assert.Equal(t, uint32(i)*rowsPerNode, row)
		}
	}
	t.Logf("%s -- %d ops", time.Since(st), len(queries))
}
