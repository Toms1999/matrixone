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

package moengine

import (
	"context"

	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/txn/client"
	"github.com/matrixorigin/matrixone/pkg/txn/rpc"
)

type wrappedEngine struct {
	engine TxnEngine
}

func EngineToTxnClient(engine TxnEngine) client.TxnClient {
	return &wrappedEngine{
		engine: engine,
	}
}

var _ client.TxnClient = new(wrappedEngine)

func (w *wrappedEngine) New(options ...client.TxnOption) (client.TxnOperator, error) {
	tx, err := w.engine.StartTxn(nil)
	if err != nil {
		panic(err)
	}
	return &wrappedTx{
		tx: tx,
	}, nil
}

func (w *wrappedEngine) NewWithSnapshot(snapshot []byte) (client.TxnOperator, error) {
	tx, err := w.engine.StartTxn(snapshot)
	if err != nil {
		return nil, err
	}
	return &wrappedTx{
		tx: tx,
	}, nil
}

type wrappedTx struct {
	tx Txn
}

func TxnToTxnOperator(tx Txn) client.TxnOperator {
	return &wrappedTx{
		tx: tx,
	}
}

var _ client.TxnOperator = new(wrappedTx)

func (w *wrappedTx) ApplySnapshot(data []byte) error {
	panic("should not call")
}

func (w *wrappedTx) Commit(ctx context.Context) error {
	return w.tx.Commit()
}

func (*wrappedTx) Read(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {
	panic("should not call")
}

func (w *wrappedTx) Rollback(ctx context.Context) error {
	return w.tx.Rollback()
}

func (*wrappedTx) Snapshot() ([]byte, error) {
	panic("should not call")
}

func (*wrappedTx) Write(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {
	panic("should not call")
}

func (*wrappedTx) WriteAndCommit(ctx context.Context, ops []txn.TxnRequest) (*rpc.SendResult, error) {
	panic("should not call")
}

func (w *wrappedTx) Txn() txn.TxnMeta {
	return txn.TxnMeta{
		ID: w.tx.GetCtx(),
	}
}
