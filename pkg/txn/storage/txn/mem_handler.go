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

package txnstorage

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"
	"github.com/matrixorigin/matrixone/pkg/pb/timestamp"
	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
	txnengine "github.com/matrixorigin/matrixone/pkg/vm/engine/txn"
	"github.com/matrixorigin/matrixone/pkg/vm/mheap"
)

type MemHandler struct {
	defaultIsolationPolicy IsolationPolicy

	// catalog
	databases  *Table[Text, DatabaseRow]
	relations  *Table[Text, RelationRow]
	attributes *Table[Text, AttributeRow]
	indexes    *Table[Text, IndexRow]

	// transactions
	transactions struct {
		sync.Mutex
		// transaction id -> transaction
		Map map[string]*Transaction
	}

	// tables
	tables struct {
		sync.Mutex
		// relation id -> table
		Map map[string]*Table[AnyKey, *AnyRow]
	}

	// iterators
	iterators struct {
		sync.Mutex
		// iterator id -> iterator
		Map map[string]*Iter[AnyKey, *AnyRow]
	}

	// misc
	mheap *mheap.Mheap
}

type Iter[
	K Ordered[K],
	R Row[K],
] struct {
	TableIter   *TableIter[K, R]
	AttrsMap    map[string]*AttributeRow
	FirstCalled bool
}

func NewMemHandler(
	mheap *mheap.Mheap,
	defaultIsolationPolicy IsolationPolicy,
) *MemHandler {
	h := &MemHandler{}
	h.transactions.Map = make(map[string]*Transaction)
	h.tables.Map = make(map[string]*Table[AnyKey, *AnyRow])
	h.iterators.Map = make(map[string]*Iter[AnyKey, *AnyRow])
	h.databases = NewTable[Text, DatabaseRow]()
	h.relations = NewTable[Text, RelationRow]()
	h.attributes = NewTable[Text, AttributeRow]()
	h.indexes = NewTable[Text, IndexRow]()
	h.mheap = mheap
	h.defaultIsolationPolicy = defaultIsolationPolicy
	return h
}

var _ Handler = new(MemHandler)

func (m *MemHandler) HandleAddTableDef(meta txn.TxnMeta, req txnengine.AddTableDefReq, resp *txnengine.AddTableDefResp) error {
	tx := m.getTx(meta)

	maxAttributeOrder := 0
	iter := m.attributes.NewIter(tx)
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.RelationID != req.TableID {
			continue
		}
		if row.Order > maxAttributeOrder {
			maxAttributeOrder = row.Order
		}
	}

	switch def := req.Def.(type) {

	case *engine.CommentDef:
		// update comments
		row, err := m.relations.Get(tx, Text(req.TableID))
		if errors.Is(err, sql.ErrNoRows) {
			resp.ErrTableNotFound.ID = req.TableID
			return nil
		}
		if err != nil {
			return err
		}
		row.Comments = def.Comment
		if err := m.relations.Update(tx, *row); err != nil {
			return err
		}

	case *engine.AttributeDef:
		// add attribute
		// check existence
		iter := m.attributes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			_, row, err := iter.Read()
			if err != nil {
				return err
			}
			if row.RelationID == req.TableID &&
				row.Name == def.Attr.Name {
				resp.ErrExisted = true
				return nil
			}
		}
		// insert
		attrRow := AttributeRow{
			ID:         uuid.NewString(),
			RelationID: req.TableID,
			Order:      maxAttributeOrder + 1,
			Attribute:  def.Attr,
		}
		if err := m.attributes.Insert(tx, attrRow); err != nil {
			return err
		}

	case *engine.IndexTableDef:
		// add index
		// check existence
		iter := m.indexes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			_, row, err := iter.Read()
			if err != nil {
				return err
			}
			if row.RelationID == req.TableID &&
				row.Name == def.Name {
				resp.ErrExisted = true
				return nil
			}
		}
		// insert
		idxRow := IndexRow{
			ID:            uuid.NewString(),
			RelationID:    req.TableID,
			IndexTableDef: *def,
		}
		if err := m.indexes.Insert(tx, idxRow); err != nil {
			return err
		}

	case *engine.PropertiesDef:
		// update properties
		row, err := m.relations.Get(tx, Text(req.TableID))
		if errors.Is(err, sql.ErrNoRows) {
			resp.ErrTableNotFound.ID = req.TableID
			return nil
		}
		for _, prop := range def.Properties {
			row.Properties[prop.Key] = prop.Value
		}
		if err := m.relations.Update(tx, *row); err != nil {
			return err
		}

	case *engine.PrimaryIndexDef:
		// set primary index
		iter := m.attributes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			_, attrRow, err := iter.Read()
			if err != nil {
				return err
			}
			if attrRow.RelationID != req.TableID {
				continue
			}
			isPrimary := false
			for _, name := range def.Names {
				if name == attrRow.Name {
					isPrimary = true
					break
				}
			}
			if isPrimary == attrRow.Primary {
				continue
			}
			attrRow.Primary = isPrimary
			if err := m.attributes.Update(tx, *attrRow); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("unknown table def: %T", req.Def)

	}

	return nil
}

func (m *MemHandler) HandleCloseTableIter(meta txn.TxnMeta, req txnengine.CloseTableIterReq, resp *txnengine.CloseTableIterResp) error {
	m.iterators.Lock()
	defer m.iterators.Unlock()
	iter, ok := m.iterators.Map[req.IterID]
	if !ok {
		resp.ErrIterNotFound.ID = req.IterID
		return nil
	}
	delete(m.iterators.Map, req.IterID)
	if err := iter.TableIter.Close(); err != nil {
		return err
	}
	return nil
}

func (m *MemHandler) HandleCreateDatabase(meta txn.TxnMeta, req txnengine.CreateDatabaseReq, resp *txnengine.CreateDatabaseResp) error {
	tx := m.getTx(meta)
	iter := m.databases.NewIter(tx)
	defer iter.Close()
	existed := false
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.Name == req.Name {
			existed = true
			break
		}
	}
	if existed {
		resp.ErrExisted = true
		return nil
	}
	err := m.databases.Insert(tx, DatabaseRow{
		ID:   uuid.NewString(),
		Name: req.Name,
	})
	if err != nil {
		return err
	}
	return nil
}

func (m *MemHandler) HandleCreateRelation(meta txn.TxnMeta, req txnengine.CreateRelationReq, resp *txnengine.CreateRelationResp) error {
	tx := m.getTx(meta)

	// validate database id
	_, err := m.databases.Get(tx, Text(req.DatabaseID))
	if errors.Is(err, sql.ErrNoRows) {
		resp.ErrDatabaseNotFound.ID = req.DatabaseID
		return nil
	}
	if err != nil {
		return err
	}

	// check existence
	iter := m.relations.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.DatabaseID == req.DatabaseID &&
			row.Name == req.Name {
			resp.ErrExisted = true
			return nil
		}
	}

	// row
	row := RelationRow{
		ID:         uuid.NewString(),
		DatabaseID: req.DatabaseID,
		Name:       req.Name,
		Type:       req.Type,
		Properties: make(map[string]string),
	}

	// handle defs
	var relAttrs []engine.Attribute
	var relIndexes []engine.IndexTableDef
	var primaryColumnNames []string
	for _, def := range req.Defs {
		switch def := def.(type) {

		case *engine.CommentDef:
			row.Comments = def.Comment

		case *engine.AttributeDef:
			relAttrs = append(relAttrs, def.Attr)

		case *engine.IndexTableDef:
			relIndexes = append(relIndexes, *def)

		case *engine.PropertiesDef:
			for _, prop := range def.Properties {
				row.Properties[prop.Key] = prop.Value
			}

		case *engine.PrimaryIndexDef:
			primaryColumnNames = def.Names

		}
	}

	// add version column
	relAttrs = append(relAttrs, engine.Attribute{
		IsHidden: true,
		IsRowId:  true,
		Name:     rowVersionColumnName,
		Type:     types.T_decimal128.ToType(),
		Default: &plan.Default{
			NullAbility: true,
		},
	})

	// insert relation attributes
	for i, attr := range relAttrs {
		if len(primaryColumnNames) > 0 {
			isPrimary := false
			for _, name := range primaryColumnNames {
				if name == attr.Name {
					isPrimary = true
					break
				}
			}
			attr.Primary = isPrimary
		}
		attrRow := AttributeRow{
			ID:         uuid.NewString(),
			RelationID: row.ID,
			Order:      i + 1,
			Attribute:  attr,
		}
		if err := m.attributes.Insert(tx, attrRow); err != nil {
			return err
		}
	}

	// insert relation indexes
	for _, idx := range relIndexes {
		idxRow := IndexRow{
			ID:            uuid.NewString(),
			RelationID:    row.ID,
			IndexTableDef: idx,
		}
		if err := m.indexes.Insert(tx, idxRow); err != nil {
			return err
		}
	}

	// insert relation
	if err := m.relations.Insert(tx, row); err != nil {
		return err
	}

	// create table
	m.tables.Lock()
	defer m.tables.Unlock()
	m.tables.Map[row.ID] = NewTable[AnyKey, *AnyRow]()

	return nil
}

const rowVersionColumnName = "__version"

func (m *MemHandler) HandleDelTableDef(meta txn.TxnMeta, req txnengine.DelTableDefReq, resp *txnengine.DelTableDefResp) error {
	tx := m.getTx(meta)
	switch def := req.Def.(type) {

	case *engine.CommentDef:
		// del comments
		row, err := m.relations.Get(tx, Text(req.TableID))
		if errors.Is(err, sql.ErrNoRows) {
			resp.ErrTableNotFound.ID = req.TableID
			return nil
		}
		if err != nil {
			return err
		}
		row.Comments = ""
		if err := m.relations.Update(tx, *row); err != nil {
			return err
		}

	case *engine.AttributeDef:
		// delete attribute
		iter := m.attributes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			key, row, err := iter.Read()
			if err != nil {
				return err
			}
			if row.RelationID == req.TableID &&
				row.Name == def.Attr.Name {
				if err := m.attributes.Delete(tx, key); err != nil {
					return err
				}
			}
		}

	case *engine.IndexTableDef:
		// delete index
		iter := m.indexes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			key, row, err := iter.Read()
			if err != nil {
				return err
			}
			if row.RelationID == req.TableID &&
				row.Name == def.Name {
				if err := m.indexes.Delete(tx, key); err != nil {
					return err
				}
			}
		}

	case *engine.PropertiesDef:
		// delete properties
		row, err := m.relations.Get(tx, Text(req.TableID))
		if errors.Is(err, sql.ErrNoRows) {
			resp.ErrTableNotFound.ID = req.TableID
			return nil
		}
		for _, prop := range def.Properties {
			delete(row.Properties, prop.Key)
		}
		if err := m.relations.Update(tx, *row); err != nil {
			return err
		}

	case *engine.PrimaryIndexDef:
		// delete primary index
		iter := m.attributes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			_, attrRow, err := iter.Read()
			if err != nil {
				return err
			}
			if attrRow.RelationID != req.TableID {
				continue
			}
			if !attrRow.Primary {
				continue
			}
			attrRow.Primary = false
			if err := m.attributes.Update(tx, *attrRow); err != nil {
				return err
			}
		}

	default:
		return fmt.Errorf("unknown table def: %T", req.Def)

	}

	return nil
}

func (m *MemHandler) HandleDelete(meta txn.TxnMeta, req txnengine.DeleteReq, resp *txnengine.DeleteResp) error {
	m.tables.Lock()
	table, ok := m.tables.Map[req.TableID]
	m.tables.Unlock()
	if !ok {
		resp.ErrTableNotFound.ID = req.TableID
		return nil
	}

	tx := m.getTx(meta)
	for i := 0; i < req.Vector.Length; i++ {
		primaryKey := AnyKey{
			typeConv(vectorAt(req.Vector, i)),
		}
		if err := table.Delete(tx, primaryKey); err != nil {
			return err
		}
	}

	return nil
}

func (m *MemHandler) HandleDeleteDatabase(meta txn.TxnMeta, req txnengine.DeleteDatabaseReq, resp *txnengine.DeleteDatabaseResp) error {
	tx := m.getTx(meta)
	iter := m.databases.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		key, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.Name != req.Name {
			continue
		}
		if err := m.databases.Delete(tx, key); err != nil {
			return err
		}
		return nil
	}
	resp.ErrNotFound.Name = req.Name
	return nil
}

func (m *MemHandler) HandleDeleteRelation(meta txn.TxnMeta, req txnengine.DeleteRelationReq, resp *txnengine.DeleteRelationResp) error {
	tx := m.getTx(meta)
	iter := m.relations.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		key, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.DatabaseID != req.DatabaseID ||
			row.Name != req.Name {
			continue
		}
		if err := m.relations.Delete(tx, key); err != nil {
			return err
		}
		return nil
	}
	resp.ErrNotFound.Name = req.Name
	return nil
}

func (m *MemHandler) HandleGetDatabases(meta txn.TxnMeta, req txnengine.GetDatabasesReq, resp *txnengine.GetDatabasesResp) error {
	tx := m.getTx(meta)
	iter := m.databases.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		resp.Names = append(resp.Names, row.Name)
	}
	return nil
}

func (m *MemHandler) HandleGetPrimaryKeys(meta txn.TxnMeta, req txnengine.GetPrimaryKeysReq, resp *txnengine.GetPrimaryKeysResp) error {
	tx := m.getTx(meta)
	iter := m.attributes.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.RelationID != req.TableID {
			continue
		}
		if !row.Primary {
			continue
		}
		resp.Attrs = append(resp.Attrs, &row.Attribute)
	}
	return nil
}

func (m *MemHandler) HandleGetRelations(meta txn.TxnMeta, req txnengine.GetRelationsReq, resp *txnengine.GetRelationsResp) error {
	tx := m.getTx(meta)
	iter := m.relations.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.DatabaseID != req.DatabaseID {
			continue
		}
		resp.Names = append(resp.Names, row.Name)
	}
	return nil
}

func (m *MemHandler) HandleGetTableDefs(meta txn.TxnMeta, req txnengine.GetTableDefsReq, resp *txnengine.GetTableDefsResp) error {
	tx := m.getTx(meta)

	relRow, err := m.relations.Get(tx, Text(req.TableID))
	if errors.Is(err, sql.ErrNoRows) {
		resp.ErrTableNotFound.ID = req.TableID
		return nil
	}
	if err != nil {
		return err
	}

	// comments
	resp.Defs = append(resp.Defs, &engine.CommentDef{
		Comment: relRow.Comments,
	})

	// attributes and primary index
	{
		var primaryAttrNames []string
		iter := m.attributes.NewIter(tx)
		defer iter.Close()
		var attrRows []*AttributeRow
		for ok := iter.First(); ok; ok = iter.Next() {
			_, attrRow, err := iter.Read()
			if err != nil {
				return err
			}
			if attrRow.RelationID != req.TableID {
				continue
			}
			attrRows = append(attrRows, attrRow)
			if attrRow.Primary {
				primaryAttrNames = append(primaryAttrNames, attrRow.Name)
			}
		}
		if len(primaryAttrNames) > 0 {
			resp.Defs = append(resp.Defs, &engine.PrimaryIndexDef{
				Names: primaryAttrNames,
			})
		}
		sort.Slice(attrRows, func(i, j int) bool {
			return attrRows[i].Order < attrRows[j].Order
		})
		for _, row := range attrRows {
			resp.Defs = append(resp.Defs, &engine.AttributeDef{
				Attr: row.Attribute,
			})

		}
	}

	// indexes
	{
		iter := m.indexes.NewIter(tx)
		defer iter.Close()
		for ok := iter.First(); ok; ok = iter.Next() {
			_, indexRow, err := iter.Read()
			if err != nil {
				return err
			}
			if indexRow.RelationID != req.TableID {
				continue
			}
			resp.Defs = append(resp.Defs, &indexRow.IndexTableDef)
		}
	}

	// properties
	propertiesDef := new(engine.PropertiesDef)
	for key, value := range relRow.Properties {
		propertiesDef.Properties = append(propertiesDef.Properties, engine.Property{
			Key:   key,
			Value: value,
		})
	}
	resp.Defs = append(resp.Defs, propertiesDef)

	return nil
}

func (m *MemHandler) HandleNewTableIter(meta txn.TxnMeta, req txnengine.NewTableIterReq, resp *txnengine.NewTableIterResp) error {
	tx := m.getTx(meta)

	m.tables.Lock()
	defer m.tables.Unlock()
	table, ok := m.tables.Map[req.TableID]
	if !ok {
		resp.ErrTableNotFound.ID = req.TableID
		return nil
	}

	tableIter := table.NewIter(tx)
	attrsMap := make(map[string]*AttributeRow)
	attrIter := m.attributes.NewIter(tx)
	for ok := attrIter.First(); ok; ok = attrIter.Next() {
		_, row, err := attrIter.Read()
		if err != nil {
			return err
		}
		if row.RelationID != req.TableID {
			continue
		}
		attrsMap[row.Name] = row
	}

	iter := &Iter[AnyKey, *AnyRow]{
		TableIter: tableIter,
		AttrsMap:  attrsMap,
	}

	m.iterators.Lock()
	defer m.iterators.Unlock()
	id := uuid.NewString()
	resp.IterID = id
	m.iterators.Map[id] = iter

	return nil
}

func (m *MemHandler) HandleOpenDatabase(meta txn.TxnMeta, req txnengine.OpenDatabaseReq, resp *txnengine.OpenDatabaseResp) error {
	tx := m.getTx(meta)
	iter := m.databases.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.Name == req.Name {
			resp.ID = row.ID
			return nil
		}
	}
	resp.ErrNotFound.Name = req.Name
	return nil
}

func (m *MemHandler) HandleOpenRelation(meta txn.TxnMeta, req txnengine.OpenRelationReq, resp *txnengine.OpenRelationResp) error {
	tx := m.getTx(meta)
	iter := m.relations.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, row, err := iter.Read()
		if err != nil {
			return err
		}
		if row.DatabaseID == req.DatabaseID &&
			row.Name == req.Name {
			resp.ID = row.ID
			resp.Type = row.Type
			return nil
		}
	}
	resp.ErrNotFound.Name = req.Name
	return nil
}

func (m *MemHandler) HandleRead(meta txn.TxnMeta, req txnengine.ReadReq, resp *txnengine.ReadResp) error {
	m.iterators.Lock()
	iter, ok := m.iterators.Map[req.IterID]
	if !ok {
		m.iterators.Unlock()
		resp.ErrIterNotFound.ID = req.IterID
		return nil
	}
	m.iterators.Unlock()

	b := batch.New(false, req.ColNames)

	for i, name := range req.ColNames {
		b.Vecs[i] = vector.New(iter.AttrsMap[name].Type)
	}

	fn := iter.TableIter.First
	if iter.FirstCalled {
		fn = iter.TableIter.Next
	} else {
		iter.FirstCalled = true
	}
	maxRows := 4096
	rows := 0

	for ok := fn(); ok; ok = iter.TableIter.Next() {
		_, row, err := iter.TableIter.Read()
		if err != nil {
			return err
		}

		for i, name := range req.ColNames {
			value, ok := (*row).attributes[iter.AttrsMap[name].ID]
			if !ok {
				resp.ErrColumnNotFound.Name = name
				return nil
			}
			b.Vecs[i].Append(value, m.mheap)
		}
		rows++

		if rows >= maxRows {
			break
		}
	}

	if rows > 0 {
		b.InitZsOne(rows)
		resp.Batch = b
	}

	return nil
}

func (m *MemHandler) HandleTruncate(meta txn.TxnMeta, req txnengine.TruncateReq, resp *txnengine.TruncateResp) error {
	tx := m.getTx(meta)
	_, err := m.relations.Get(tx, Text(req.TableID))
	if errors.Is(err, sql.ErrNoRows) {
		resp.ErrTableNotFound.ID = req.TableID
		return nil
	}
	m.tables.Lock()
	defer m.tables.Unlock()
	m.tables.Map[req.TableID] = NewTable[AnyKey, *AnyRow]()
	return nil
}

func (m *MemHandler) HandleUpdate(meta txn.TxnMeta, req txnengine.UpdateReq, resp *txnengine.UpdateResp) error {
	tx := m.getTx(meta)

	if err := m.rangeBatchPhysicalRows(
		tx,
		req.TableID,
		req.Batch,
		&resp.ErrTableNotFound,
		func(
			table *Table[AnyKey, *AnyRow],
			row *AnyRow,
		) error {
			if err := table.Update(tx, row); err != nil {
				return err
			}
			return nil
		},
	); err != nil {
		return err
	}

	return nil
}

func (m *MemHandler) HandleWrite(meta txn.TxnMeta, req txnengine.WriteReq, resp *txnengine.WriteResp) error {
	tx := m.getTx(meta)

	if err := m.rangeBatchPhysicalRows(
		tx,
		req.TableID,
		req.Batch,
		&resp.ErrTableNotFound,
		func(
			table *Table[AnyKey, *AnyRow],
			row *AnyRow,
		) error {
			if err := table.Insert(tx, row); err != nil {
				return err
			}
			return nil
		},
	); err != nil {
		return err
	}

	return nil
}

func (m *MemHandler) rangeBatchPhysicalRows(
	tx *Transaction,
	tableID string,
	b *batch.Batch,
	errTableNotFound *txnengine.ErrRelationNotFound,
	fn func(
		*Table[AnyKey, *AnyRow],
		*AnyRow,
	) error,
) error {

	// load attributes
	nameToAttrs := make(map[string]*AttributeRow)
	iter := m.attributes.NewIter(tx)
	defer iter.Close()
	for ok := iter.First(); ok; ok = iter.Next() {
		_, attrRow, err := iter.Read()
		if err != nil {
			return err
		}
		if attrRow.RelationID != tableID {
			continue
		}
		nameToAttrs[attrRow.Name] = attrRow
	}

	if len(nameToAttrs) == 0 {
		errTableNotFound.ID = tableID
		return nil
	}

	// write
	m.tables.Lock()
	table, ok := m.tables.Map[tableID]
	m.tables.Unlock()
	if !ok {
		panic("impossible")
	}
	batchIter := NewBatchIter(b)
	for {
		row := batchIter()
		if len(row) == 0 {
			break
		}

		physicalRow := NewAnyRow()

		for i, col := range row {
			name := b.Attrs[i]

			attr, ok := nameToAttrs[name]
			if !ok {
				return fmt.Errorf("unknown attr: %s", name)
			}

			if attr.Primary {
				physicalRow.primaryKey = append(physicalRow.primaryKey, typeConv(col))
			}

			physicalRow.attributes[attr.ID] = col
		}

		// add version
		a := rand.Int63()
		b := rand.Int63()
		version := types.Decimal128FromInt64Raw(a, b)
		physicalRow.attributes[nameToAttrs[rowVersionColumnName].ID] = version

		if err := fn(table, physicalRow); err != nil {
			return err
		}

	}

	return nil
}

func (m *MemHandler) getTx(meta txn.TxnMeta) *Transaction {
	id := string(meta.ID)
	m.transactions.Lock()
	defer m.transactions.Unlock()
	tx, ok := m.transactions.Map[id]
	if !ok {
		tx = NewTransaction(id, meta.SnapshotTS, m.defaultIsolationPolicy)
		m.transactions.Map[id] = tx
	}
	return tx
}

func (*MemHandler) HandleClose() error {
	return nil
}

func (m *MemHandler) HandleCommit(meta txn.TxnMeta) error {
	tx := m.getTx(meta)
	tx.State.Store(Committed)
	return nil
}

func (m *MemHandler) HandleCommitting(meta txn.TxnMeta) error {
	return nil
}

func (m *MemHandler) HandleDestroy() error {
	*m = *NewMemHandler(m.mheap, m.defaultIsolationPolicy)
	return nil
}

func (m *MemHandler) HandlePrepare(meta txn.TxnMeta) (timestamp.Timestamp, error) {
	tx := m.getTx(meta)
	tx.Tick()
	return tx.CurrentTime, nil
}

func (m *MemHandler) HandleRollback(meta txn.TxnMeta) error {
	tx := m.getTx(meta)
	tx.State.Store(Aborted)
	return nil
}

func (m *MemHandler) HandleStartRecovery(ch chan txn.TxnMeta) {
	// no recovery
	close(ch)
}
