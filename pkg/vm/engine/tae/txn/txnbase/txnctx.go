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

package txnbase

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/matrixorigin/matrixone/pkg/container/types"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
)

func IDToIDCtx(id uint64) []byte {
	ctx := make([]byte, 8)
	binary.BigEndian.PutUint64(ctx, id)
	return ctx
}

func IDCtxToID(buf []byte) uint64 {
	return binary.BigEndian.Uint64(buf)
}

type TxnCtx struct {
	sync.RWMutex
	DoneCond                     sync.Cond
	ID                           uint64
	IDCtx                        []byte
	StartTS, CommitTS, PrepareTS types.TS
	Info                         []byte
	State                        txnif.TxnState
	Is2PC                        bool
}

func NewTxnCtx(id uint64, start types.TS, info []byte) *TxnCtx {
	ctx := &TxnCtx{
		ID:       id,
		IDCtx:    IDToIDCtx(id),
		StartTS:  start,
		CommitTS: txnif.UncommitTS,
		Info:     info,
	}
	ctx.DoneCond = *sync.NewCond(ctx)
	return ctx
}

func (ctx *TxnCtx) GetCtx() []byte {
	return ctx.IDCtx
}

func (ctx *TxnCtx) Repr() string {
	ctx.RLock()
	defer ctx.RUnlock()
	repr := fmt.Sprintf("ctx[%d][%d->%d][%s]", ctx.ID, ctx.StartTS, ctx.CommitTS, txnif.TxnStrState(ctx.State))
	return repr
}

func (ctx *TxnCtx) SameTxn(startTs types.TS) bool { return ctx.StartTS.Equal(startTs) }
func (ctx *TxnCtx) CommitBefore(startTs types.TS) bool {
	return ctx.GetCommitTS().Less(startTs)
}
func (ctx *TxnCtx) CommitAfter(startTs types.TS) bool {
	return ctx.GetCommitTS().Greater(startTs)
}

func (ctx *TxnCtx) String() string       { return ctx.Repr() }
func (ctx *TxnCtx) GetID() uint64        { return ctx.ID }
func (ctx *TxnCtx) GetInfo() []byte      { return ctx.Info }
func (ctx *TxnCtx) GetStartTS() types.TS { return ctx.StartTS }
func (ctx *TxnCtx) GetCommitTS() types.TS {
	ctx.RLock()
	defer ctx.RUnlock()
	return ctx.CommitTS
}
func (ctx *TxnCtx) GetPrepareTS() types.TS {
	ctx.RLock()
	defer ctx.RUnlock()
	return ctx.PrepareTS
}

// Atomically returns the current txn state
func (ctx *TxnCtx) getTxnState() txnif.TxnState {
	ctx.RLock()
	defer ctx.RUnlock()
	return ctx.State
}

// Wait txn state to be rollbacked or committed
func (ctx *TxnCtx) resolveTxnState() txnif.TxnState {
	ctx.DoneCond.L.Lock()
	defer ctx.DoneCond.L.Unlock()
	state := ctx.State
	if state != txnif.TxnStateCommitting {
		return state
	}
	ctx.DoneCond.Wait()
	return ctx.State
}

// False when atomically get the current txn state
//
// True when the txn state is committing, wait it to be committed or rollbacked. It
// is used during snapshot reads. If TxnStateActive is currently returned, this value will
// definitely not be used, because even if it becomes TxnStateCommitting later, the timestamp
// would be larger than the current read timestamp.
func (ctx *TxnCtx) GetTxnState(waitIfcommitting bool) (state txnif.TxnState) {
	// Quick get the current txn state
	// If waitIfcommitting is false, return the state
	// If state is not txnif.TxnStateCommitting, return the state
	if state = ctx.getTxnState(); !waitIfcommitting || state != txnif.TxnStateCommitting {
		return state
	}

	// Wait the committing txn to be committed or rollbacked
	state = ctx.resolveTxnState()
	return
}

func (ctx *TxnCtx) IsVisible(o txnif.TxnReader) bool {
	ostart := o.GetStartTS()
	ctx.RLock()
	defer ctx.RUnlock()
	return ostart.LessEq(ctx.StartTS)
}

func (ctx *TxnCtx) IsActiveLocked() bool {
	return ctx.CommitTS == txnif.UncommitTS
}

func (ctx *TxnCtx) ToPreparingLocked(ts types.TS) error {
	if ts.LessEq(ctx.StartTS) {
		panic(fmt.Sprintf("start ts %d should be less than commit ts %d", ctx.StartTS, ts))
	}
	if !ctx.CommitTS.Equal(txnif.UncommitTS) {
		return ErrTxnNotActive
	}
	ctx.PrepareTS = ts
	ctx.CommitTS = ts
	ctx.State = txnif.TxnStatePreparing
	return nil
}

func (ctx *TxnCtx) ToCommittingLocked(ts types.TS) error {
	if ts.LessEq(ctx.StartTS) {
		panic(fmt.Sprintf("start ts %d should be less than commit ts %d", ctx.StartTS, ts))
	}
	if !ctx.CommitTS.Equal(txnif.UncommitTS) {
		return ErrTxnNotActive
	}
	ctx.CommitTS = ts
	ctx.PrepareTS = ts
	ctx.State = txnif.TxnStateCommitting
	return nil
}

func (ctx *TxnCtx) ToCommittedLocked() error {
	if ctx.State != txnif.TxnStateCommitting {
		return ErrTxnNotCommitting
	}
	ctx.State = txnif.TxnStateCommitted
	return nil
}

func (ctx *TxnCtx) ToRollbackingLocked(ts types.TS) error {
	if ts.LessEq(ctx.StartTS) {
		panic(fmt.Sprintf("start ts %d should be less than commit ts %d", ctx.StartTS, ts))
	}
	if (ctx.State != txnif.TxnStateActive) && (ctx.State != txnif.TxnStateCommitting) {
		return ErrTxnCannotRollback
	}
	ctx.CommitTS = ts
	ctx.PrepareTS = ts
	ctx.State = txnif.TxnStateRollbacking
	return nil
}

func (ctx *TxnCtx) ToRollbackedLocked() error {
	if ctx.State != txnif.TxnStateRollbacking {
		return ErrTxnNotRollbacking
	}
	ctx.State = txnif.TxnStateRollbacked
	return nil
}

func (ctx *TxnCtx) ToUnknownLocked() {
	ctx.State = txnif.TxnStateUnknown
}

// MockSetCommitTSLocked is for testing
func (ctx *TxnCtx) MockSetCommitTSLocked(ts types.TS) { ctx.CommitTS = ts }
