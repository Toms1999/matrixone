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
	"fmt"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/matrixorigin/matrixone/pkg/container/batch"
	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/container/vector"
	"github.com/matrixorigin/matrixone/pkg/frontend"
	"github.com/matrixorigin/matrixone/pkg/pb/timestamp"
	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
	txnengine "github.com/matrixorigin/matrixone/pkg/vm/engine/txn"
)

// CatalogHandler handles read-only requests for catalog
type CatalogHandler struct {
	upstream   *MemHandler
	database   *DatabaseRow
	relations  map[string]*RelationRow  // id -> row
	attributes map[string]*AttributeRow // id -> row
	iterators  struct {
		sync.Mutex
		Map map[string]any // id -> Iterator
	}
}

var _ Handler = new(CatalogHandler)

func NewCatalogHandler(upstream *MemHandler) *CatalogHandler {

	handler := &CatalogHandler{
		upstream:   upstream,
		relations:  make(map[string]*RelationRow),
		attributes: make(map[string]*AttributeRow),
	}
	handler.iterators.Map = make(map[string]any)

	// database
	handler.database = &DatabaseRow{
		ID:   uuid.NewString(),
		Name: "mo_catalog", // hardcoded in frontend package
	}

	// relations
	databasesRelRow := &RelationRow{
		ID:         uuid.NewString(),
		DatabaseID: handler.database.ID,
		Name:       "mo_database", // hardcoded in frontend package
		Type:       txnengine.RelationTable,
	}
	handler.relations[databasesRelRow.ID] = databasesRelRow
	tablesRelRow := &RelationRow{
		ID:         uuid.NewString(),
		DatabaseID: handler.database.ID,
		Name:       "mo_tables", // hardcoded in frontend package
		Type:       txnengine.RelationTable,
	}
	handler.relations[tablesRelRow.ID] = tablesRelRow
	attributesRelRow := &RelationRow{
		ID:         uuid.NewString(),
		DatabaseID: handler.database.ID,
		Name:       "mo_columns", // hardcoded in frontend package
		Type:       txnengine.RelationTable,
	}
	handler.relations[attributesRelRow.ID] = attributesRelRow

	// attributes
	// databases
	for i, def := range frontend.ConvertCatalogSchemaToEngineFormat(
		frontend.DefineSchemaForMoDatabase(),
	) {
		row := &AttributeRow{
			ID:         uuid.NewString(),
			RelationID: databasesRelRow.ID,
			Order:      i,
			Attribute:  def.Attr,
		}
		handler.attributes[row.ID] = row
	}
	// relations
	for i, def := range frontend.ConvertCatalogSchemaToEngineFormat(
		frontend.DefineSchemaForMoTables(),
	) {
		row := &AttributeRow{
			ID:         uuid.NewString(),
			RelationID: tablesRelRow.ID,
			Order:      i,
			Attribute:  def.Attr,
		}
		handler.attributes[row.ID] = row
	}
	// attributes
	for i, def := range frontend.ConvertCatalogSchemaToEngineFormat(
		frontend.DefineSchemaForMoColumns(),
	) {
		row := &AttributeRow{
			ID:         uuid.NewString(),
			RelationID: attributesRelRow.ID,
			Order:      i,
			Attribute:  def.Attr,
		}
		handler.attributes[row.ID] = row
	}

	return handler
}

func (c *CatalogHandler) HandleAddTableDef(meta txn.TxnMeta, req txnengine.AddTableDefReq, resp *txnengine.AddTableDefResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
		return nil
	}
	return c.upstream.HandleAddTableDef(meta, req, resp)
}

func (c *CatalogHandler) HandleClose() error {
	return c.upstream.HandleClose()
}

func (c *CatalogHandler) HandleCloseTableIter(meta txn.TxnMeta, req txnengine.CloseTableIterReq, resp *txnengine.CloseTableIterResp) error {

	c.iterators.Lock()
	v, ok := c.iterators.Map[req.IterID]
	c.iterators.Unlock()
	if ok {
		switch v := v.(type) {
		case *Iter[Text, DatabaseRow]:
			if err := v.TableIter.Close(); err != nil {
				return err
			}
		case *Iter[Text, RelationRow]:
			if err := v.TableIter.Close(); err != nil {
				return err
			}
		case *Iter[Text, AttributeRow]:
			if err := v.TableIter.Close(); err != nil {
				return err
			}
		default:
			panic(fmt.Errorf("fixme: %T", v))
		}
		c.iterators.Lock()
		delete(c.iterators.Map, req.IterID)
		c.iterators.Unlock()
		return nil
	}

	return c.upstream.HandleCloseTableIter(meta, req, resp)
}

func (c *CatalogHandler) HandleCommit(meta txn.TxnMeta) error {
	return c.upstream.HandleCommit(meta)
}

func (c *CatalogHandler) HandleCommitting(meta txn.TxnMeta) error {
	return c.upstream.HandleCommitting(meta)
}

func (c *CatalogHandler) HandleCreateDatabase(meta txn.TxnMeta, req txnengine.CreateDatabaseReq, resp *txnengine.CreateDatabaseResp) error {
	if req.Name == c.database.Name {
		resp.ErrReadOnly.Why = req.Name + " is system database"
		return nil
	}
	return c.upstream.HandleCreateDatabase(meta, req, resp)
}

func (c *CatalogHandler) HandleCreateRelation(meta txn.TxnMeta, req txnengine.CreateRelationReq, resp *txnengine.CreateRelationResp) error {
	if req.DatabaseID == c.database.ID {
		resp.ErrReadOnly.Why = "can't create relation in system database"
		return nil
	}
	return c.upstream.HandleCreateRelation(meta, req, resp)
}

func (c *CatalogHandler) HandleDelTableDef(meta txn.TxnMeta, req txnengine.DelTableDefReq, resp *txnengine.DelTableDefResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
		return nil
	}
	return c.upstream.HandleDelTableDef(meta, req, resp)
}

func (c *CatalogHandler) HandleDelete(meta txn.TxnMeta, req txnengine.DeleteReq, resp *txnengine.DeleteResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
		return nil
	}
	return c.upstream.HandleDelete(meta, req, resp)
}

func (c *CatalogHandler) HandleDeleteDatabase(meta txn.TxnMeta, req txnengine.DeleteDatabaseReq, resp *txnengine.DeleteDatabaseResp) error {
	if req.Name == c.database.Name {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system database", req.Name)
		return nil
	}
	return c.upstream.HandleDeleteDatabase(meta, req, resp)
}

func (c *CatalogHandler) HandleDeleteRelation(meta txn.TxnMeta, req txnengine.DeleteRelationReq, resp *txnengine.DeleteRelationResp) error {
	if req.DatabaseID == c.database.ID {
		resp.ErrReadOnly.Why = "can't delete relation from system database"
		return nil
	}
	return c.upstream.HandleDeleteRelation(meta, req, resp)
}

func (c *CatalogHandler) HandleDestroy() error {
	return c.upstream.HandleDestroy()
}

func (c *CatalogHandler) HandleGetDatabases(meta txn.TxnMeta, req txnengine.GetDatabasesReq, resp *txnengine.GetDatabasesResp) error {
	if err := c.upstream.HandleGetDatabases(meta, req, resp); err != nil {
		return err
	}
	resp.Names = append(resp.Names, c.database.Name)
	return nil
}

func (c *CatalogHandler) HandleGetPrimaryKeys(meta txn.TxnMeta, req txnengine.GetPrimaryKeysReq, resp *txnengine.GetPrimaryKeysResp) error {
	if rel, ok := c.relations[req.TableID]; ok {
		for _, attr := range c.attributes {
			if attr.RelationID != rel.ID {
				continue
			}
			if !attr.Primary {
				continue
			}
			resp.Attrs = append(resp.Attrs, &attr.Attribute)
		}
		return nil
	}
	return c.upstream.HandleGetPrimaryKeys(meta, req, resp)
}

func (c *CatalogHandler) HandleGetRelations(meta txn.TxnMeta, req txnengine.GetRelationsReq, resp *txnengine.GetRelationsResp) error {
	if req.DatabaseID == c.database.ID {
		for _, row := range c.relations {
			resp.Names = append(resp.Names, row.Name)
		}
		return nil
	}
	return c.upstream.HandleGetRelations(meta, req, resp)
}

func (c *CatalogHandler) HandleGetTableDefs(meta txn.TxnMeta, req txnengine.GetTableDefsReq, resp *txnengine.GetTableDefsResp) error {
	if rel, ok := c.relations[req.TableID]; ok {

		// comments
		resp.Defs = append(resp.Defs, &engine.CommentDef{
			Comment: rel.Comments,
		})

		// attributes and primary index
		{
			var primaryAttrNames []string
			var attrRows []*AttributeRow
			for _, attr := range c.attributes {
				if attr.RelationID != req.TableID {
					continue
				}
				attrRows = append(attrRows, attr)
				if attr.Primary {
					primaryAttrNames = append(primaryAttrNames, attr.Name)
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

		// properties
		propertiesDef := new(engine.PropertiesDef)
		for key, value := range rel.Properties {
			propertiesDef.Properties = append(propertiesDef.Properties, engine.Property{
				Key:   key,
				Value: value,
			})
		}
		resp.Defs = append(resp.Defs, propertiesDef)

		return nil
	}

	return c.upstream.HandleGetTableDefs(meta, req, resp)
}

func (c *CatalogHandler) HandleNewTableIter(meta txn.TxnMeta, req txnengine.NewTableIterReq, resp *txnengine.NewTableIterResp) error {

	if rel, ok := c.relations[req.TableID]; ok {
		tx := c.upstream.getTx(meta)

		attrsMap := make(map[string]*AttributeRow)
		for _, attr := range c.attributes {
			if attr.RelationID != req.TableID {
				continue
			}
			attrsMap[attr.Name] = attr
		}

		var iter any
		switch rel.Name {
		case "mo_database":
			iter = &Iter[Text, DatabaseRow]{
				TableIter: c.upstream.databases.NewIter(tx),
				AttrsMap:  attrsMap,
			}
		case "mo_tables":
			iter = &Iter[Text, RelationRow]{
				TableIter: c.upstream.relations.NewIter(tx),
				AttrsMap:  attrsMap,
			}
		case "mo_columns":
			iter = &Iter[Text, AttributeRow]{
				TableIter: c.upstream.attributes.NewIter(tx),
				AttrsMap:  attrsMap,
			}
		default:
			panic(fmt.Errorf("fixme: %s", rel.Name))
		}

		id := uuid.NewString()
		resp.IterID = id
		c.iterators.Lock()
		c.iterators.Map[id] = iter
		c.iterators.Unlock()

		return nil
	}

	return c.upstream.HandleNewTableIter(meta, req, resp)
}

func (c *CatalogHandler) HandleOpenDatabase(meta txn.TxnMeta, req txnengine.OpenDatabaseReq, resp *txnengine.OpenDatabaseResp) error {
	if req.Name == c.database.Name {
		resp.ID = c.database.ID
		return nil
	}
	return c.upstream.HandleOpenDatabase(meta, req, resp)
}

func (c *CatalogHandler) HandleOpenRelation(meta txn.TxnMeta, req txnengine.OpenRelationReq, resp *txnengine.OpenRelationResp) error {
	if req.DatabaseID == c.database.ID {
		for _, row := range c.relations {
			if row.Name == req.Name {
				resp.ID = row.ID
				resp.Type = row.Type
				return nil
			}
		}
		resp.ErrNotFound.Name = req.Name
		return nil
	}
	return c.upstream.HandleOpenRelation(meta, req, resp)
}

func (c *CatalogHandler) HandlePrepare(meta txn.TxnMeta) (timestamp.Timestamp, error) {
	return c.upstream.HandlePrepare(meta)
}

func (c *CatalogHandler) HandleRead(meta txn.TxnMeta, req txnengine.ReadReq, resp *txnengine.ReadResp) error {

	c.iterators.Lock()
	v, ok := c.iterators.Map[req.IterID]
	c.iterators.Unlock()
	if ok {
		b := batch.New(false, req.ColNames)
		maxRows := 4096
		rows := 0

		switch iter := v.(type) {

		case *Iter[Text, DatabaseRow]:
			for i, name := range req.ColNames {
				b.Vecs[i] = vector.New(iter.AttrsMap[name].Type)
			}

			fn := iter.TableIter.First
			if iter.FirstCalled {
				fn = iter.TableIter.Next
			} else {
				iter.FirstCalled = true
			}
			for ok := fn(); ok; ok = iter.TableIter.Next() {
				_, row, err := iter.TableIter.Read()
				if err != nil {
					return err
				}
				for i, name := range req.ColNames {
					var value any

					switch name {
					case "datname":
						value = row.Name
					case "dat_catalog_name":
						value = ""
					case "dat_createsql":
						value = ""
					default:
						resp.ErrColumnNotFound.Name = name
						return nil
					}

					b.Vecs[i].Append(value, c.upstream.mheap)
				}
				rows++
				if rows >= maxRows {
					break
				}
			}

		case *Iter[Text, RelationRow]:
			for i, name := range req.ColNames {
				b.Vecs[i] = vector.New(iter.AttrsMap[name].Type)
			}

			fn := iter.TableIter.First
			if iter.FirstCalled {
				fn = iter.TableIter.Next
			} else {
				iter.FirstCalled = true
			}
			for ok := fn(); ok; ok = iter.TableIter.Next() {
				_, row, err := iter.TableIter.Read()
				if err != nil {
					return err
				}
				for i, name := range req.ColNames {
					var value any

					switch name {
					case "relname":
						value = row.Name
					case "reldatabase":
						value = c.database.Name
					case "relpersistence":
						value = true
					case "relkind":
						value = "r"
					case "rel_comment":
						value = row.Comments
					case "rel_createsql":
						value = ""
					default:
						resp.ErrColumnNotFound.Name = name
						return nil
					}

					b.Vecs[i].Append(value, c.upstream.mheap)
				}
				rows++
				if rows >= maxRows {
					break
				}
			}

		case *Iter[Text, AttributeRow]:
			for i, name := range req.ColNames {
				b.Vecs[i] = vector.New(iter.AttrsMap[name].Type)
			}

			fn := iter.TableIter.First
			if iter.FirstCalled {
				fn = iter.TableIter.Next
			} else {
				iter.FirstCalled = true
			}
			for ok := fn(); ok; ok = iter.TableIter.Next() {
				_, row, err := iter.TableIter.Read()
				if err != nil {
					return err
				}
				for i, name := range req.ColNames {
					var value any

					switch name {
					case "att_database":
						value = c.database.Name
					case "att_relname":
						rel := c.relations[row.RelationID]
						value = rel.Name
					case "attname":
						value = row.Name
					case "atttyp":
						value = row.Type.Oid
					case "attnum":
						value = row.Order
					case "att_length":
						value = row.Type.Size
					case "attnotnull":
						value = row.Default.NullAbility
					case "atthasdef":
						value = row.Default.Expr != nil
					case "att_default":
						value = row.Default.Expr.String()
					case "attisdropped":
						value = false
					case "att_constraint_type":
						if row.Primary {
							value = "p"
						} else {
							value = "n"
						}
					case "att_is_unsigned":
						value = row.Type.Oid == types.T_uint8 ||
							row.Type.Oid == types.T_uint16 ||
							row.Type.Oid == types.T_uint32 ||
							row.Type.Oid == types.T_uint64 ||
							row.Type.Oid == types.T_uint128
					case "att_is_auto_increment":
						value = false
					case "att_comment":
						value = row.Comment
					case "att_is_hidden":
						value = row.IsHidden
					default:
						resp.ErrColumnNotFound.Name = name
						return nil
					}

					b.Vecs[i].Append(value, c.upstream.mheap)
				}
				rows++
				if rows >= maxRows {
					break
				}
			}

		default:
			panic(fmt.Errorf("fixme: %T", v))
		}

		if rows > 0 {
			b.InitZsOne(rows)
			resp.Batch = b
		}

		return nil
	}

	return c.upstream.HandleRead(meta, req, resp)
}

func (c *CatalogHandler) HandleRollback(meta txn.TxnMeta) error {
	return c.upstream.HandleRollback(meta)
}

func (c *CatalogHandler) HandleStartRecovery(ch chan txn.TxnMeta) {
	c.upstream.HandleStartRecovery(ch)
}

func (c *CatalogHandler) HandleTruncate(meta txn.TxnMeta, req txnengine.TruncateReq, resp *txnengine.TruncateResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
	}
	return c.upstream.HandleTruncate(meta, req, resp)
}

func (c *CatalogHandler) HandleUpdate(meta txn.TxnMeta, req txnengine.UpdateReq, resp *txnengine.UpdateResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
	}
	return c.upstream.HandleUpdate(meta, req, resp)
}

func (c *CatalogHandler) HandleWrite(meta txn.TxnMeta, req txnengine.WriteReq, resp *txnengine.WriteResp) error {
	if row, ok := c.relations[req.TableID]; ok {
		resp.ErrReadOnly.Why = fmt.Sprintf("%s is system table", row.Name)
	}
	return c.upstream.HandleWrite(meta, req, resp)
}
