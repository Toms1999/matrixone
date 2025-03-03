// Copyright 2022 Matrix Origin
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package plan

import (
	"github.com/matrixorigin/matrixone/pkg/errno"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/sql/errors"
	"github.com/matrixorigin/matrixone/pkg/sql/parsers/tree"
)

func NewPartitionBinder(builder *QueryBuilder, ctx *BindContext) *PartitionBinder {
	p := &PartitionBinder{}
	p.builder = builder
	p.ctx = ctx
	p.impl = p
	return p
}

func (p *PartitionBinder) BindExpr(expr tree.Expr, i int32, b bool) (*plan.Expr, error) {
	return p.baseBindExpr(expr, i, b)
}

func (p *PartitionBinder) BindColRef(name *tree.UnresolvedName, i int32, b bool) (*plan.Expr, error) {
	return p.baseBindColRef(name, i, b)
}

func (p *PartitionBinder) BindAggFunc(s string, expr *tree.FuncExpr, i int32, b bool) (*plan.Expr, error) {
	return nil, errors.New(errno.GroupingError, "aggregate functions not allowed here")
}

func (p *PartitionBinder) BindWinFunc(s string, expr *tree.FuncExpr, i int32, b bool) (*plan.Expr, error) {
	return nil, errors.New(errno.WindowingError, "window functions not allowed here")
}

func (p *PartitionBinder) BindSubquery(subquery *tree.Subquery, b bool) (*plan.Expr, error) {
	return nil, errors.New(errno.InvalidColumnReference, "subquery not allowed here")
}
