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

package disttae

import (
	"context"
	"time"

	"github.com/matrixorigin/matrixone/pkg/common/moerr"
	"github.com/matrixorigin/matrixone/pkg/pb/logservice"
	"github.com/matrixorigin/matrixone/pkg/txn/client"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
)

type GetClusterDetailsFunc = func() (logservice.ClusterDetails, error)

func New(
	ctx context.Context,
	getClusterDetails GetClusterDetailsFunc,
) *Engine {
	return &Engine{
		getClusterDetails: getClusterDetails,
		txns:              make(map[string]*Transaction),
	}
}

var _ engine.Engine = new(Engine)

func (e *Engine) Create(ctx context.Context, name string, op client.TxnOperator) error {
	//TODO
	e.newTransaction(op)
	panic("unimplemented")
}

func (e *Engine) Database(ctx context.Context, name string, op client.TxnOperator) (engine.Database, error) {
	//TODO
	e.newTransaction(op)
	panic("unimplemented")
}

func (e *Engine) Databases(ctx context.Context, op client.TxnOperator) ([]string, error) {
	//TODO
	e.newTransaction(op)
	panic("unimplemented")
}

func (e *Engine) Delete(ctx context.Context, name string, op client.TxnOperator) error {
	//TODO
	e.newTransaction(op)
	panic("unimplemented")
}

// hasConflict used to detect if a transaction on a cn is in conflict,
// currently an empty implementation, assuming all transactions on a cn are conflict free
func (e *Engine) HasConflict(txn *Transaction) bool {
	return false
}

func (e *Engine) PreCommit(ctx context.Context, op client.TxnOperator) error {
	txn := e.getTransaction(op)
	if txn == nil {
		return moerr.New(moerr.ErrTxnClosed, "the transaction has been committed or aborted")
	}
	defer e.delTransaction(txn)
	if e.HasConflict(txn) {
		return moerr.New(moerr.ErrTxnWriteConflict, "write conflict")
	}
	return nil
}

func (e *Engine) Rollback(ctx context.Context, op client.TxnOperator) error {
	txn := e.getTransaction(op)
	if txn == nil {
		return moerr.New(moerr.ErrTxnClosed, "the transaction has been committed or aborted")
	}
	defer e.delTransaction(txn)
	return nil
}

func (e *Engine) Nodes() (engine.Nodes, error) {
	clusterDetails, err := e.getClusterDetails()
	if err != nil {
		return nil, err
	}

	var nodes engine.Nodes
	for _, store := range clusterDetails.CNStores {
		nodes = append(nodes, engine.Node{
			Mcpu: 1,
			Id:   store.UUID,
			Addr: store.ServiceAddress,
		})
	}

	return nodes, nil
}

func (e *Engine) Hints() (h engine.Hints) {
	h.CommitOrRollbackTimeout = time.Minute * 5
	return
}

func (e *Engine) newTransaction(op client.TxnOperator) {
	txn := &Transaction{
		readOnly: false,
		meta:     op.Txn(),
	}
	e.addTransaction(txn)
}

func (e *Engine) getTransaction(op client.TxnOperator) *Transaction {
	e.Lock()
	defer e.Unlock()
	return e.txns[string(op.Txn().ID)]
}

func (e *Engine) addTransaction(txn *Transaction) {
	e.Lock()
	defer e.Unlock()
	if _, ok := e.txns[string(txn.meta.ID)]; !ok {
		e.txns[string(txn.meta.ID)] = txn
	}
}

func (e *Engine) delTransaction(txn *Transaction) {
	e.Lock()
	defer e.Unlock()
	delete(e.txns, string(txn.meta.ID))
}
