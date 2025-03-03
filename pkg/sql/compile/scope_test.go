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

package compile

import (
	"context"
	"fmt"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/dialect/mysql"
	plan2 "github.com/matrixorigin/matrixone/pkg/sql/plan"
	"github.com/matrixorigin/matrixone/pkg/testutil"
	"github.com/matrixorigin/matrixone/pkg/vm"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/memEngine"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestScopeSerialization(t *testing.T) {
	testCases := []string{
		"select 1",
		"select * from R",
		"select count(*) from R",
		"select * from R limit 2, 1",
		"select * from R left join S on R.uid = S.uid",
	}

	var sourceScopes = generateScopeCases(t, testCases)

	for i, sourceScope := range sourceScopes {
		data, errEncode := encodeScope(sourceScope)
		require.NoError(t, errEncode)
		targetScope, errDecode := decodeScope(data, sourceScope.Proc)
		require.NoError(t, errDecode)

		// Just do simple check
		require.Equal(t, len(sourceScope.PreScopes), len(targetScope.PreScopes), fmt.Sprintf("related SQL is '%s'", testCases[i]))
		require.Equal(t, len(sourceScope.Instructions), len(targetScope.Instructions), fmt.Sprintf("related SQL is '%s'", testCases[i]))
		for j := 0; j < len(sourceScope.Instructions); j++ {
			require.Equal(t, sourceScope.Instructions[j].Op, targetScope.Instructions[j].Op)
		}
		if sourceScope.DataSource == nil {
			require.Nil(t, targetScope.DataSource)
		} else {
			require.Equal(t, sourceScope.DataSource.SchemaName, targetScope.DataSource.SchemaName)
			require.Equal(t, sourceScope.DataSource.RelationName, targetScope.DataSource.RelationName)
			require.Equal(t, sourceScope.DataSource.PushdownId, targetScope.DataSource.PushdownId)
			require.Equal(t, sourceScope.DataSource.PushdownAddr, targetScope.DataSource.PushdownAddr)
		}
		require.Equal(t, sourceScope.NodeInfo.Addr, targetScope.NodeInfo.Addr)
		require.Equal(t, sourceScope.NodeInfo.Id, targetScope.NodeInfo.Id)
	}

}

func generateScopeCases(t *testing.T, testCases []string) []*Scope {
	// getScope method generate and return the scope of a SQL string.
	getScope := func(t1 *testing.T, sql string) *Scope {
		proc := testutil.NewProcess()
		e := memEngine.NewTestEngine()
		opt := plan2.NewBaseOptimizer(e.(*memEngine.MemEngine))
		stmts, err := mysql.Parse(sql)
		require.NoError(t1, err)
		qry, err := opt.Optimize(stmts[0])
		require.NoError(t1, err)
		c := New("test", sql, "", context.Background(), e, proc, nil)
		err = c.Compile(&plan.Plan{Plan: &plan.Plan_Query{Query: qry}}, nil, func(a any, batch *batch.Batch) error {
			return nil
		})
		require.NoError(t1, err)
		// ignore the last operator if it's output
		if c.scope.Instructions[len(c.scope.Instructions)-1].Op == vm.Output {
			c.scope.Instructions = c.scope.Instructions[:len(c.scope.Instructions)-1]
		}
		return c.scope
	}

	result := make([]*Scope, len(testCases))
	for i, sql := range testCases {
		result[i] = getScope(t, sql)
	}
	return result
}
