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

package catalog

import (
	"bytes"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/logutil"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/common"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/file"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/logstore/driver/batchstoredriver"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/logstore/store"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/tasks"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/txn/txnbase"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/wal"
)

// +--------+---------+----------+----------+------------+
// |   ID   |  Name   | CreateAt | DeleteAt | CommitInfo |
// +--------+---------+----------+----------+------------+
// |(uint64)|(varchar)| (uint64) | (uint64) |  (varchar) |
// +--------+---------+----------+----------+------------+

type DataFactory interface {
	MakeTableFactory() TableDataFactory
	MakeSegmentFactory() SegmentDataFactory
	MakeBlockFactory(segFile file.Segment) BlockDataFactory
}

type Catalog struct {
	*IDAlloctor
	*sync.RWMutex
	store store.Store

	scheduler   tasks.TaskScheduler
	ckpmu       sync.RWMutex
	checkpoints []*Checkpoint

	entries   map[uint64]*common.GenericDLNode[*DBEntry]
	nameNodes map[string]*nodeList[*DBEntry]
	link      *common.GenericSortedDList[*DBEntry]

	nodesMu sync.RWMutex

	tableCnt  int32
	columnCnt int32
}

func genDBFullName(tenantID uint32, name string) string {
	if name == SystemDBName {
		tenantID = 0
	}
	return fmt.Sprintf("%d-%s", tenantID, name)
}

func compareDBFn(a, b *DBEntry) int {
	return a.BaseEntry.DoCompre(b.BaseEntry)
}

func MockCatalog(dir, name string, cfg *batchstoredriver.StoreCfg, scheduler tasks.TaskScheduler) *Catalog {
	driver := store.NewStoreWithBatchStoreDriver(dir, name, cfg)
	catalog := &Catalog{
		RWMutex:     new(sync.RWMutex),
		IDAlloctor:  NewIDAllocator(),
		store:       driver,
		entries:     make(map[uint64]*common.GenericDLNode[*DBEntry]),
		nameNodes:   make(map[string]*nodeList[*DBEntry]),
		link:        common.NewGenericSortedDList(compareDBFn),
		checkpoints: make([]*Checkpoint, 0),
		scheduler:   scheduler,
	}
	catalog.InitSystemDB()
	return catalog
}

func OpenCatalog(dir, name string, cfg *batchstoredriver.StoreCfg, scheduler tasks.TaskScheduler, dataFactory DataFactory) (*Catalog, error) {
	driver := store.NewStoreWithBatchStoreDriver(dir, name, cfg)
	catalog := &Catalog{
		RWMutex:     new(sync.RWMutex),
		IDAlloctor:  NewIDAllocator(),
		store:       driver,
		entries:     make(map[uint64]*common.GenericDLNode[*DBEntry]),
		nameNodes:   make(map[string]*nodeList[*DBEntry]),
		link:        common.NewGenericSortedDList(compareDBFn),
		checkpoints: make([]*Checkpoint, 0),
		scheduler:   scheduler,
	}
	catalog.InitSystemDB()
	replayer := NewReplayer(dataFactory, catalog)
	err := catalog.store.Replay(replayer.ReplayerHandle)
	return catalog, err
}

func (catalog *Catalog) InitSystemDB() {
	sysDB := NewSystemDBEntry(catalog)
	dbTables := NewSystemTableEntry(sysDB, SystemTable_DB_ID, SystemDBSchema)
	tableTables := NewSystemTableEntry(sysDB, SystemTable_Table_ID, SystemTableSchema)
	columnTables := NewSystemTableEntry(sysDB, SystemTable_Columns_ID, SystemColumnSchema)
	err := sysDB.AddEntryLocked(dbTables, nil)
	if err != nil {
		panic(err)
	}
	if err = sysDB.AddEntryLocked(tableTables, nil); err != nil {
		panic(err)
	}
	if err = sysDB.AddEntryLocked(columnTables, nil); err != nil {
		panic(err)
	}
	if err = catalog.AddEntryLocked(sysDB, nil); err != nil {
		panic(err)
	}
}

func (catalog *Catalog) GetStore() store.Store { return catalog.store }

func (catalog *Catalog) ReplayCmd(txncmd txnif.TxnCmd, dataFactory DataFactory, idxCtx *wal.Index, observer wal.ReplayObserver, cache *bytes.Buffer) {
	switch txncmd.GetType() {
	case txnbase.CmdComposed:
		cmds := txncmd.(*txnbase.ComposedCmd)
		idxCtx.Size = cmds.CmdSize
		for i, cmds := range cmds.Cmds {
			idx := idxCtx.Clone()
			idx.CSN = uint32(i)
			catalog.ReplayCmd(cmds, dataFactory, idx, observer, cache)
		}
	case CmdLogBlock:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayBlock(cmd, dataFactory)
	case CmdLogSegment:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplaySegment(cmd, dataFactory, cache)
	case CmdLogTable:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayTable(cmd, dataFactory)
	case CmdLogDatabase:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayDatabase(cmd)
	case CmdUpdateDatabase:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayUpdateDatabase(cmd, idxCtx, observer)
	case CmdUpdateTable:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayUpdateTable(cmd, dataFactory, idxCtx, observer)
	case CmdUpdateSegment:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayUpdateSegment(cmd, dataFactory, idxCtx, observer, cache)
	case CmdUpdateBlock:
		cmd := txncmd.(*EntryCommand)
		catalog.onReplayUpdateBlock(cmd, dataFactory, idxCtx, observer)
	default:
		panic("unsupport")
	}
}

// 2,3 always stale
// snapshot->ckped entry(must covered by ss)->unckped but in ss wal->wal
func (catalog *Catalog) onReplayUpdateDatabase(cmd *EntryCommand, idx *wal.Index, observer wal.ReplayObserver) {
	catalog.OnReplayDBID(cmd.DB.ID)
	if cmd.GetTs().LessEq(catalog.GetCheckpointed().MaxTS) {
		if observer != nil {
			observer.OnStaleIndex(idx)
		}
		return
	}
	var err error

	db, err := catalog.GetDatabaseByID(cmd.entry.ID)
	if err != nil {
		cmd.DB.RWMutex = new(sync.RWMutex)
		cmd.DB.catalog = catalog
		cmd.entry.GetUpdateNodeLocked().AddLogIndex(idx)
		err = catalog.AddEntryLocked(cmd.DB, nil)
		if err != nil {
			panic(err)
		}
		if observer != nil {
			observer.OnTimeStamp(cmd.GetTs())
		}
		return
	}

	un := cmd.entry.GetUpdateNodeLocked()
	un.AddLogIndex(idx)
	dbun := db.GetExactUpdateNode(un.Start)
	if dbun == nil {
		db.InsertNode(un) //TODO isvalid
	} else {
		dbun.UpdateNode(un)
	}
	if observer != nil {
		observer.OnTimeStamp(cmd.GetTs())
	}
}

func (catalog *Catalog) onReplayDatabase(cmd *EntryCommand) {
	var err error
	catalog.OnReplayDBID(cmd.DB.ID)

	db, err := catalog.GetDatabaseByID(cmd.DB.ID)
	if err != nil {
		cmd.DB.RWMutex = new(sync.RWMutex)
		cmd.DB.catalog = catalog
		err = catalog.AddEntryLocked(cmd.DB, nil)
		if err != nil {
			panic(err)
		}
		return
	}

	cmd.DB.MVCC.Loop(func(n *common.GenericDLNode[*UpdateNode]) bool {
		un := n.GetPayload()
		dbun := db.GetExactUpdateNode(un.Start)
		if dbun == nil {
			db.InsertNode(un) //TODO isvalid
		} else {
			dbun.UpdateNode(un)
		}
		return true
	}, true)
}

func (catalog *Catalog) onReplayUpdateTable(cmd *EntryCommand, dataFactory DataFactory, idx *wal.Index, observer wal.ReplayObserver) {
	catalog.OnReplayTableID(cmd.Table.ID)
	if cmd.GetTs().LessEq(catalog.GetCheckpointed().MaxTS) {
		if observer != nil {
			observer.OnStaleIndex(idx)
		}
		return
	}
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	tbl, err := db.GetTableEntryByID(cmd.Table.ID)
	if err != nil {
		cmd.Table.db = db
		cmd.Table.tableData = dataFactory.MakeTableFactory()(cmd.Table)
		cmd.entry.GetUpdateNodeLocked().AddLogIndex(idx)
		err = db.AddEntryLocked(cmd.Table, nil)
		if err != nil {
			panic(err)
		}
		if observer != nil {
			observer.OnTimeStamp(cmd.GetTs())
		}
		return
	}

	un := cmd.entry.GetUpdateNodeLocked()
	tblun := tbl.GetExactUpdateNode(un.Start)
	un.AddLogIndex(idx)
	if tblun == nil {
		tbl.InsertNode(un) //TODO isvalid
	} else {
		tblun.UpdateNode(un)
	}

	if observer != nil {
		observer.OnTimeStamp(cmd.GetTs())
	}
}

func (catalog *Catalog) onReplayTable(cmd *EntryCommand, dataFactory DataFactory) {
	catalog.OnReplayTableID(cmd.Table.ID)
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	rel, err := db.GetTableEntryByID(cmd.Table.ID)
	if err != nil {
		cmd.Table.db = db
		cmd.Table.tableData = dataFactory.MakeTableFactory()(cmd.Table)
		err = db.AddEntryLocked(cmd.Table, nil)
		if err != nil {
			panic(err)
		}
	} else {
		cmd.Table.MVCC.Loop(func(n *common.GenericDLNode[*UpdateNode]) bool {
			un := n.GetPayload()
			node := rel.GetExactUpdateNode(un.Start)
			if node == nil {
				rel.InsertNode(un)
			} else {
				node.UpdateNode(un)
			}
			return true
		}, true)
	}
}

func (catalog *Catalog) onReplayUpdateSegment(cmd *EntryCommand, dataFactory DataFactory, idx *wal.Index, observer wal.ReplayObserver, cache *bytes.Buffer) {
	catalog.OnReplaySegmentID(cmd.Segment.ID)
	if cmd.GetTs().LessEq(catalog.GetCheckpointed().MaxTS) {
		if observer != nil {
			observer.OnStaleIndex(idx)
		}
		return
	}
	cmd.Segment.GetUpdateNodeLocked().AddLogIndex(idx)
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	tbl, err := db.GetTableEntryByID(cmd.TableID)
	if err != nil {
		panic(err)
	}
	seg, err := tbl.GetSegmentByID(cmd.Segment.ID)
	if err != nil {
		cmd.Segment.table = tbl
		cmd.Segment.RWMutex = new(sync.RWMutex)
		cmd.Segment.segData = dataFactory.MakeSegmentFactory()(cmd.Segment)
		tbl.AddEntryLocked(cmd.Segment)
	} else {
		un := cmd.entry.GetUpdateNodeLocked()
		node := seg.GetExactUpdateNode(un.Start)
		if node == nil {
			seg.InsertNode(un)
		} else {
			node.UpdateNode(un)
		}
	}
	if observer != nil {
		observer.OnTimeStamp(cmd.GetTs())
	}
}

func (catalog *Catalog) onReplaySegment(cmd *EntryCommand, dataFactory DataFactory, cache *bytes.Buffer) {
	catalog.OnReplaySegmentID(cmd.Segment.ID)
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	rel, err := db.GetTableEntryByID(cmd.TableID)
	if err != nil {
		panic(err)
	}
	seg, err := rel.GetSegmentByID(cmd.Segment.ID)
	if err != nil {
		cmd.Segment.table = rel
		rel.AddEntryLocked(cmd.Segment)
	} else {
		cmd.Segment.MVCC.Loop(func(n *common.GenericDLNode[*UpdateNode]) bool {
			un := n.GetPayload()
			segun := seg.GetExactUpdateNode(un.Start)
			if segun != nil {
				segun.UpdateNode(un)
			} else {
				seg.InsertNode(un)
			}
			return true
		}, true)
	}
}

func (catalog *Catalog) onReplayUpdateBlock(cmd *EntryCommand, dataFactory DataFactory, idx *wal.Index, observer wal.ReplayObserver) {
	catalog.OnReplayBlockID(cmd.Block.ID)
	if cmd.GetTs().LessEq(catalog.GetCheckpointed().MaxTS) {
		if observer != nil {
			observer.OnStaleIndex(idx)
		}
		return
	}
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	tbl, err := db.GetTableEntryByID(cmd.TableID)
	if err != nil {
		panic(err)
	}
	seg, err := tbl.GetSegmentByID(cmd.SegmentID)
	if err != nil {
		panic(err)
	}
	blk, err := seg.GetBlockEntryByID(cmd.Block.ID)
	un := cmd.entry.GetUpdateNodeLocked()
	un.AddLogIndex(idx)
	if err == nil {
		blkun := blk.GetExactUpdateNode(un.Start)
		if blkun != nil {
			blkun.UpdateNode(un)
		} else {
			blk.InsertNode(un)
			if observer != nil {
				observer.OnTimeStamp(un.End)
			}
		}
		return
	}
	cmd.Block.RWMutex = new(sync.RWMutex)
	cmd.Block.segment = seg
	cmd.Block.blkData = dataFactory.MakeBlockFactory(seg.segData.GetSegmentFile())(cmd.Block)
	ts := cmd.Block.blkData.GetMaxCheckpointTS()
	if observer != nil {
		observer.OnTimeStamp(ts)
	}
	un.AddLogIndex(idx)
	seg.AddEntryLocked(cmd.Block)
	if observer != nil {
		observer.OnTimeStamp(un.End)
	}
}

func (catalog *Catalog) onReplayBlock(cmd *EntryCommand, dataFactory DataFactory) {
	catalog.OnReplayBlockID(cmd.Block.ID)
	db, err := catalog.GetDatabaseByID(cmd.DBID)
	if err != nil {
		panic(err)
	}
	rel, err := db.GetTableEntryByID(cmd.TableID)
	if err != nil {
		panic(err)
	}
	seg, err := rel.GetSegmentByID(cmd.SegmentID)
	if err != nil {
		panic(err)
	}
	blk, _ := seg.GetBlockEntryByID(cmd.Block.ID)
	if blk == nil {
		cmd.Block.segment = seg
		seg.AddEntryLocked(cmd.Block)
	} else {
		cmd.Block.MVCC.Loop(func(n *common.GenericDLNode[*UpdateNode]) bool {
			un := n.GetPayload()
			blkun := blk.GetExactUpdateNode(un.Start)
			if blkun != nil {
				blkun.UpdateNode(un)
			}
			blk.InsertNode(un)
			return false
		}, true)
	}
}

func (catalog *Catalog) ReplayTableRows() {
	rows := uint64(0)
	tableProcessor := new(LoopProcessor)
	tableProcessor.BlockFn = func(be *BlockEntry) error {
		if !be.IsActive() {
			return nil
		}
		rows += be.GetBlockData().GetRowsOnReplay()
		return nil
	}
	processor := new(LoopProcessor)
	processor.TableFn = func(tbl *TableEntry) error {
		if tbl.db.name == SystemDBName {
			return nil
		}
		rows = 0
		err := tbl.RecurLoop(tableProcessor)
		if err != nil {
			panic(err)
		}
		tbl.rows = rows
		return nil
	}
	err := catalog.RecurLoop(processor)
	if err != nil {
		panic(err)
	}
}
func (catalog *Catalog) Close() error {
	if catalog.store != nil {
		catalog.store.Close()
	}
	return nil
}

func (catalog *Catalog) CoarseDBCnt() int {
	catalog.RLock()
	defer catalog.RUnlock()
	return len(catalog.entries)
}

func (catalog *Catalog) CoarseTableCnt() int {
	return int(atomic.LoadInt32(&catalog.tableCnt))
}

func (catalog *Catalog) CoarseColumnCnt() int {
	return int(atomic.LoadInt32(&catalog.columnCnt))
}

func (catalog *Catalog) AddTableCnt(cnt int) {
	n := atomic.AddInt32(&catalog.tableCnt, int32(cnt))
	if n < 0 {
		panic("logic error")
	}
}

func (catalog *Catalog) AddColumnCnt(cnt int) {
	n := atomic.AddInt32(&catalog.columnCnt, int32(cnt))
	if n < 0 {
		panic("logic error")
	}
}

func (catalog *Catalog) GetItemNodeByIDLocked(id uint64) *common.GenericDLNode[*DBEntry] {
	return catalog.entries[id]
}

func (catalog *Catalog) GetScheduler() tasks.TaskScheduler { return catalog.scheduler }
func (catalog *Catalog) GetDatabaseByID(id uint64) (db *DBEntry, err error) {
	catalog.RLock()
	defer catalog.RUnlock()
	node := catalog.entries[id]
	if node == nil {
		err = ErrNotFound
		return
	}
	db = node.GetPayload()
	return
}

func (catalog *Catalog) AddEntryLocked(database *DBEntry, txn txnif.TxnReader) error {
	nn := catalog.nameNodes[database.GetFullName()]
	if nn == nil {
		n := catalog.link.Insert(database)
		catalog.entries[database.GetID()] = n

		nn := newNodeList[*DBEntry](catalog.GetItemNodeByIDLocked,
			databaseTxnCanGetFn[*DBEntry],
			&catalog.nodesMu,
			database.name)
		catalog.nameNodes[database.GetFullName()] = nn

		nn.CreateNode(database.GetID())
	} else {
		node := nn.GetNode()
		record := node.GetPayload()
		err := record.PrepareAdd(txn)
		if err != nil {
			return err
		}
		n := catalog.link.Insert(database)
		catalog.entries[database.GetID()] = n
		nn.CreateNode(database.GetID())
	}
	return nil
}

func (catalog *Catalog) MakeDBIt(reverse bool) *common.GenericSortedDListIt[*DBEntry] {
	catalog.RLock()
	defer catalog.RUnlock()
	return common.NewGenericSortedDListIt[*DBEntry](catalog.RWMutex, catalog.link, reverse)
}

func (catalog *Catalog) SimplePPString(level common.PPLevel) string {
	return catalog.PPString(level, 0, "")
}

func (catalog *Catalog) PPString(level common.PPLevel, depth int, prefix string) string {
	var w bytes.Buffer
	cnt := 0
	it := catalog.MakeDBIt(true)
	for it.Valid() {
		cnt++
		entry := it.Get().GetPayload()
		_ = w.WriteByte('\n')
		_, _ = w.WriteString(entry.PPString(level, depth+1, ""))
		it.Next()
	}

	var ckp *Checkpoint
	catalog.ckpmu.RLock()
	if len(catalog.checkpoints) > 0 {
		ckp = catalog.checkpoints[len(catalog.checkpoints)-1]
	}
	catalog.ckpmu.RUnlock()
	var w2 bytes.Buffer
	_, _ = w2.WriteString(fmt.Sprintf("CATALOG[CNT=%d][%s]", cnt, ckp.String()))
	_, _ = w2.WriteString(w.String())
	return w2.String()
}

func (catalog *Catalog) RemoveEntry(database *DBEntry) error {
	if database.IsSystemDB() {
		logutil.Warnf("system db cannot be removed")
		return ErrNotPermitted
	}
	logutil.Info("[Catalog]", common.OperationField("remove"),
		common.OperandField(database.String()))
	catalog.Lock()
	defer catalog.Unlock()
	if n, ok := catalog.entries[database.GetID()]; !ok {
		return ErrNotFound
	} else {
		nn := catalog.nameNodes[database.GetFullName()]
		nn.DeleteNode(database.GetID())
		catalog.link.Delete(n)
		if nn.Length() == 0 {
			delete(catalog.nameNodes, database.GetFullName())
		}
		delete(catalog.entries, database.GetID())
	}
	return nil
}

func (catalog *Catalog) txnGetNodeByNameLocked(name string, txnCtx txnif.AsyncTxn) (*common.GenericDLNode[*DBEntry], error) {
	catalog.RLock()
	defer catalog.RUnlock()
	fullName := genDBFullName(txnCtx.GetTenantID(), name)
	node := catalog.nameNodes[fullName]
	if node == nil {
		return nil, ErrNotFound
	}
	return node.TxnGetNodeLocked(txnCtx)
}

func (catalog *Catalog) GetDBEntry(name string, txnCtx txnif.AsyncTxn) (*DBEntry, error) {
	n, err := catalog.txnGetNodeByNameLocked(name, txnCtx)
	if err != nil {
		return nil, err
	}
	return n.GetPayload(), nil
}

func (catalog *Catalog) DropDBEntry(name string, txnCtx txnif.AsyncTxn) (deleted *DBEntry, err error) {
	if name == SystemDBName {
		err = ErrNotPermitted
		return
	}
	dn, err := catalog.txnGetNodeByNameLocked(name, txnCtx)
	if err != nil {
		return
	}
	entry := dn.GetPayload()
	entry.Lock()
	defer entry.Unlock()
	err = entry.DropEntryLocked(txnCtx)
	if err == nil {
		deleted = entry
	}
	return
}

func (catalog *Catalog) CreateDBEntry(name string, txnCtx txnif.AsyncTxn) (*DBEntry, error) {
	var err error
	catalog.Lock()
	defer catalog.Unlock()
	entry := NewDBEntry(catalog, name, txnCtx)
	err = catalog.AddEntryLocked(entry, txnCtx)

	return entry, err
}

func (catalog *Catalog) CreateDBEntryByTS(name string, ts types.TS) (*DBEntry, error) {
	entry := NewDBEntryByTS(catalog, name, ts)
	err := catalog.AddEntryLocked(entry, nil)
	return entry, err
}

func (catalog *Catalog) RecurLoop(processor Processor) (err error) {
	dbIt := catalog.MakeDBIt(true)
	for dbIt.Valid() {
		dbEntry := dbIt.Get().GetPayload()
		if err = processor.OnDatabase(dbEntry); err != nil {
			if err == ErrStopCurrRecur {
				err = nil
				dbIt.Next()
				continue
			}
			break
		}
		if err = dbEntry.RecurLoop(processor); err != nil {
			return
		}
		dbIt.Next()
	}
	if err == ErrStopCurrRecur {
		err = nil
	}
	return err
}

func (catalog *Catalog) PrepareCheckpoint(startTs, endTs types.TS) *CheckpointEntry {
	ckpEntry := NewCheckpointEntry(startTs, endTs)
	processor := new(LoopProcessor)
	processor.BlockFn = func(block *BlockEntry) (err error) {
		CheckpointOp(ckpEntry, block, startTs, endTs)
		return
	}
	processor.SegmentFn = func(segment *SegmentEntry) (err error) {
		CheckpointOp(ckpEntry, segment, startTs, endTs)
		return
	}
	processor.TableFn = func(table *TableEntry) (err error) {
		if table.IsVirtual() {
			err = ErrStopCurrRecur
			return
		}
		CheckpointOp(ckpEntry, table, startTs, endTs)
		return
	}
	processor.DatabaseFn = func(database *DBEntry) (err error) {
		if database.IsSystemDB() {
			// No need to checkpoint system db entry
			return
		}
		CheckpointOp(ckpEntry, database, startTs, endTs)
		return
	}
	if err := catalog.RecurLoop(processor); err != nil {
		panic(err)
	}
	return ckpEntry
}

func (catalog *Catalog) GetCheckpointed() *Checkpoint {
	catalog.ckpmu.RLock()
	defer catalog.ckpmu.RUnlock()
	if len(catalog.checkpoints) == 0 {
		return EmptyCheckpoint
	}
	return catalog.checkpoints[len(catalog.checkpoints)-1]
}

func (catalog *Catalog) CheckpointClosure(maxTs types.TS) tasks.FuncT {
	return func() error {
		return catalog.Checkpoint(maxTs)
	}
}
func (catalog *Catalog) NeedCheckpoint(maxTS types.TS) (needCheckpoint bool, minTS types.TS, err error) {
	catalog.ckpmu.RLock()
	defer catalog.ckpmu.RUnlock()
	if len(catalog.checkpoints) != 0 {
		lastMax := catalog.checkpoints[len(catalog.checkpoints)-1].MaxTS
		if maxTS.Less(lastMax) {
			err = ErrCheckpoint
			return
		}
		if maxTS.Equal(lastMax) {
			return
		}
		//minTs = lastMax + 1
		minTS = lastMax.Next()
	}
	needCheckpoint = true
	return
}

func (catalog *Catalog) Checkpoint(maxTs types.TS) (err error) {
	now := time.Now()
	var minTs types.TS
	var needCheckpoint bool
	if needCheckpoint, minTs, err = catalog.NeedCheckpoint(maxTs); !needCheckpoint {
		return
	}
	entry := catalog.PrepareCheckpoint(minTs, maxTs)
	logutil.Debugf("PrepareCheckpoint: %s", time.Since(now))
	if len(entry.LogIndexes) == 0 {
		return
	}
	now = time.Now()
	logEntry, err := entry.MakeLogEntry()
	if err != nil {
		return
	}
	logutil.Debugf("MakeLogEntry: %s", time.Since(now))
	now = time.Now()
	defer logEntry.Free()
	checkpoint := new(Checkpoint)
	checkpoint.MaxTS = maxTs
	checkpoint.LSN = entry.MaxIndex.LSN
	checkpoint.CommitId, err = catalog.store.Append(0, logEntry)
	if err != nil {
		panic(err)
	}
	if err = logEntry.WaitDone(); err != nil {
		panic(err)
	}
	logutil.Debugf("SaveCheckpointed: %s", time.Since(now))
	// for _, index := range entry.LogIndexes {
	// 	logutil.Debugf("Ckp0Index %s", index.String())
	// }
	now = time.Now()
	if err = catalog.scheduler.Checkpoint(entry.LogIndexes); err != nil {
		logutil.Warnf("Schedule checkpoint log indexes: %v", err)
		return
	}
	logutil.Debugf("CheckpointWal: %s", time.Since(now))
	catalog.ckpmu.Lock()
	catalog.checkpoints = append(catalog.checkpoints, checkpoint)
	catalog.ckpmu.Unlock()
	logutil.Debugf("Max LogIndex: %s", entry.MaxIndex.String())
	return
}
