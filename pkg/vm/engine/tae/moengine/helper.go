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
	"fmt"
	"strings"

	"github.com/matrixorigin/matrixone/pkg/logutil/logutil2"
	"github.com/matrixorigin/matrixone/pkg/pb/plan"

	"github.com/matrixorigin/matrixone/pkg/vm/engine"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/catalog"
	"github.com/matrixorigin/matrixone/pkg/vm/engine/tae/iface/txnif"
)

type TenantIDKey struct{}
type UserIDKey struct{}
type RoleIDKey struct{}

func txnBindAccessInfoFromCtx(txn txnif.AsyncTxn, ctx context.Context) {
	if ctx == nil {
		return
	}
	tid, okt := ctx.Value(TenantIDKey{}).(uint32)
	uid, oku := ctx.Value(UserIDKey{}).(uint32)
	rid, okr := ctx.Value(RoleIDKey{}).(uint32)
	logutil2.Debugf(ctx, "try set %d txn access info to t%d(%v) u%d(%v) r%d(%v), ", txn.GetID(), tid, okt, uid, oku, rid, okr)
	if okt { // TODO: tenantID is required, or all need to be ok?
		txn.BindAccessInfo(tid, uid, rid)
	}
}

func SchemaToDefs(schema *catalog.Schema) (defs []engine.TableDef, err error) {
	if schema.Comment != "" {
		commentDef := new(engine.CommentDef)
		commentDef.Comment = schema.Comment
		defs = append(defs, commentDef)
	}

	if schema.View != "" {
		viewDef := new(engine.ViewDef)
		viewDef.View = schema.View
		defs = append(defs, viewDef)
	}
	for _, col := range schema.ColDefs {
		if col.IsPhyAddr() {
			continue
		}

		expr := &plan.Expr{}
		if col.Default.Expr != nil {
			if err := expr.Unmarshal(col.Default.Expr); err != nil {
				return nil, err
			}
		} else {
			expr = nil
		}

		def := &engine.AttributeDef{
			Attr: engine.Attribute{
				Name:    col.Name,
				Type:    col.Type,
				Primary: col.IsPrimary(),
				Comment: col.Comment,
				Default: &plan.Default{
					NullAbility:  col.Default.NullAbility,
					OriginString: col.Default.OriginString,
					Expr:         expr,
				},
				AutoIncrement: col.IsAutoIncrement(),
			},
		}
		defs = append(defs, def)
	}
	if schema.SortKey != nil && schema.SortKey.IsPrimary() {
		pk := new(engine.PrimaryIndexDef)
		for _, def := range schema.SortKey.Defs {
			pk.Names = append(pk.Names, def.Name)
		}
		defs = append(defs, pk)
	}
	pro := new(engine.PropertiesDef)
	pro.Properties = append(pro.Properties, engine.Property{
		Key:   catalog.SystemRelAttr_Kind,
		Value: string(schema.Relkind),
	})
	if schema.Createsql != "" {
		pro.Properties = append(pro.Properties, engine.Property{
			Key:   catalog.SystemRelAttr_CreateSQL,
			Value: schema.Createsql,
		})
	}
	defs = append(defs, pro)

	return
}

func DefsToSchema(name string, defs []engine.TableDef) (schema *catalog.Schema, err error) {
	schema = catalog.NewEmptySchema(name)
	pkMap := make(map[string]int)
	for _, def := range defs {
		if pkDef, ok := def.(*engine.PrimaryIndexDef); ok {
			for i, name := range pkDef.Names {
				pkMap[name] = i
			}
			break
		}
	}
	for _, def := range defs {
		switch defVal := def.(type) {
		case *engine.AttributeDef:
			if idx, ok := pkMap[defVal.Attr.Name]; ok {
				if err = schema.AppendPKColWithAttribute(defVal.Attr, idx); err != nil {
					return
				}
			} else {
				if err = schema.AppendColWithAttribute(defVal.Attr); err != nil {
					return
				}
			}

		case *engine.PropertiesDef:
			for _, property := range defVal.Properties {
				switch strings.ToLower(property.Key) {
				case catalog.SystemRelAttr_Comment:
					schema.Comment = property.Value
				case catalog.SystemRelAttr_Kind:
					schema.Relkind = property.Value
				case catalog.SystemRelAttr_CreateSQL:
					schema.Createsql = property.Value
				default:
				}
			}

		case *engine.ViewDef:
			schema.View = defVal.View

		default:
			// We will not deal with other cases for the time being
		}
	}
	if err = schema.Finalize(false); err != nil {
		return
	}
	if schema.IsCompoundSortKey() {
		err = fmt.Errorf("%w: compound idx not supported yet", catalog.ErrSchemaValidation)
	}
	return
}
