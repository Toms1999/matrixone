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

package txnimpl

import (
	"fmt"
	"sync"

	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/catalog"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/handle"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/txn/txnbase"
)

type txnDBIt struct {
	*sync.RWMutex
	txn    txnif.AsyncTxn
	linkIt *common.GenericSortedDListIt[*catalog.DBEntry]
	itered bool // linkIt has no dummy head, use this to avoid duplicate filter logic for the very first entry
	curr   *catalog.DBEntry
	err    error
}

func newDBIt(txn txnif.AsyncTxn, c *catalog.Catalog) *txnDBIt {
	it := &txnDBIt{
		RWMutex: c.RWMutex,
		txn:     txn,
		linkIt:  c.MakeDBIt(true),
	}
	it.Next()
	return it
}

func (it *txnDBIt) Close() error    { return nil }
func (it *txnDBIt) GetError() error { return it.err }
func (it *txnDBIt) Valid() bool {
	if it.err != nil {
		return false
	}
	return it.linkIt.Valid()
}

func (it *txnDBIt) Next() {
	var err error
	var valid bool
	for {
		if it.itered {
			it.linkIt.Next()
		}
		node := it.linkIt.Get()
		it.itered = true
		if node == nil {
			it.curr = nil
			break
		}
		curr := node.GetPayload()
		curr.RLock()
		if curr.GetTenantID() == it.txn.GetTenantID() || isSysSharedDB(curr.GetName()) {
			valid, err = curr.TxnCanRead(it.txn, curr.RWMutex)
		}
		curr.RUnlock()
		if err != nil {
			it.err = err
			break
		}
		if valid {
			it.curr = curr
			break
		}
	}
}

func (it *txnDBIt) GetCurr() *catalog.DBEntry { return it.curr }

type txnDatabase struct {
	*txnbase.TxnDatabase
	txnDB *txnDB
}

func newDatabase(db *txnDB) *txnDatabase {
	dbase := &txnDatabase{
		TxnDatabase: &txnbase.TxnDatabase{
			Txn: db.store.txn,
		},
		txnDB: db,
	}
	return dbase

}
func (db *txnDatabase) GetID() uint64   { return db.txnDB.entry.GetID() }
func (db *txnDatabase) GetName() string { return db.txnDB.entry.GetName() }
func (db *txnDatabase) String() string  { return db.txnDB.entry.String() }

func (db *txnDatabase) CreateRelation(def any) (rel handle.Relation, err error) {
	return db.Txn.GetStore().CreateRelation(db.txnDB.entry.ID, def)
}

func (db *txnDatabase) DropRelationByName(name string) (rel handle.Relation, err error) {
	return db.Txn.GetStore().DropRelationByName(db.txnDB.entry.ID, name)
}

func (db *txnDatabase) TruncateByName(name string) (rel handle.Relation, err error) {
	old, err := db.DropRelationByName(name)
	if err != nil {
		err = fmt.Errorf("%w: truncate %s error", err, name)
		return
	}
	meta := old.GetMeta().(*catalog.TableEntry)
	schema := meta.GetSchema().Clone()
	rel, err = db.CreateRelation(schema)
	if err != nil {
		err = fmt.Errorf("%w: truncate %s error", err, name)
	}
	return
}

func (db *txnDatabase) GetRelationByName(name string) (rel handle.Relation, err error) {
	return db.Txn.GetStore().GetRelationByName(db.txnDB.entry.ID, name)
}

func (db *txnDatabase) MakeRelationIt() (it handle.RelationIt) {
	return newRelationIt(db.txnDB)
}

func (db *txnDatabase) RelationCnt() int64                  { return 0 }
func (db *txnDatabase) Relations() (rels []handle.Relation) { return }
func (db *txnDatabase) Close() error                        { return nil }
func (db *txnDatabase) GetMeta() any                        { return db.txnDB.entry }
