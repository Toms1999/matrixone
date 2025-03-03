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
	"bytes"
	"context"
	"encoding/gob"
	"testing"

	"github.com/matrixorigin/matrixone/pkg/container/types"
	"github.com/matrixorigin/matrixone/pkg/pb/timestamp"
	"github.com/matrixorigin/matrixone/pkg/pb/txn"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
	txnengine "github.com/matrixorigin/matrixone/pkg/vm/engine/txn"
	"github.com/stretchr/testify/assert"
)

func testDatabase(
	t *testing.T,
	newStorage func() (*Storage, error),
) {

	// new
	s, err := newStorage()
	assert.Nil(t, err)
	defer s.Close(context.TODO())

	// txn
	txnMeta := txn.TxnMeta{
		ID:     []byte("1"),
		Status: txn.TxnStatus_Active,
		SnapshotTS: timestamp.Timestamp{
			PhysicalTime: 1,
			LogicalTime:  1,
		},
	}

	// open database
	{
		resp := testRead[txnengine.OpenDatabaseResp](
			t, s, txnMeta,
			txnengine.OpOpenDatabase,
			txnengine.OpenDatabaseReq{
				Name: "foo",
			},
		)
		assert.Equal(t, "foo", resp.ErrNotFound.Name)
	}

	// create database
	{
		resp := testWrite[txnengine.CreateDatabaseResp](
			t, s, txnMeta,
			txnengine.OpCreateDatabase,
			txnengine.CreateDatabaseReq{
				Name: "foo",
			},
		)
		assert.Equal(t, txnengine.ErrExisted(false), resp.ErrExisted)
	}

	// get databases
	{
		resp := testRead[txnengine.GetDatabasesResp](
			t, s, txnMeta,
			txnengine.OpGetDatabases,
			txnengine.GetDatabasesReq{},
		)
		assert.Equal(t, 1, len(resp.Names))
		assert.Equal(t, "foo", resp.Names[0])
	}

	// open database
	var dbID string
	{
		resp := testRead[txnengine.OpenDatabaseResp](
			t, s, txnMeta,
			txnengine.OpOpenDatabase,
			txnengine.OpenDatabaseReq{
				Name: "foo",
			},
		)
		assert.Equal(t, "", resp.ErrNotFound.Name)
		assert.NotNil(t, resp.ID)
		dbID = resp.ID

		// delete database
		defer func() {
			{
				resp := testWrite[txnengine.DeleteDatabaseResp](
					t, s, txnMeta,
					txnengine.OpDeleteDatabase,
					txnengine.DeleteDatabaseReq{
						Name: "foo",
					},
				)
				assert.Equal(t, "", resp.ErrNotFound.Name)
			}
			{
				resp := testRead[txnengine.GetDatabasesResp](
					t, s, txnMeta,
					txnengine.OpGetDatabases,
					txnengine.GetDatabasesReq{},
				)
				assert.Equal(t, 0, len(resp.Names))
			}
		}()
	}

	// open relation
	{
		resp := testRead[txnengine.OpenRelationResp](
			t, s, txnMeta,
			txnengine.OpOpenRelation,
			txnengine.OpenRelationReq{
				DatabaseID: dbID,
				Name:       "table",
			},
		)
		assert.Equal(t, "table", resp.ErrNotFound.Name)
	}

	// create relation
	{
		resp := testWrite[txnengine.CreateRelationResp](
			t, s, txnMeta,
			txnengine.OpCreateRelation,
			txnengine.CreateRelationReq{
				DatabaseID: dbID,
				Name:       "table",
				Type:       txnengine.RelationTable,
				Defs: []engine.TableDef{
					&engine.AttributeDef{
						Attr: engine.Attribute{
							Name:    "a",
							Type:    types.New(types.T_int64, 0, 0, 0),
							Primary: true,
						},
					},
					&engine.AttributeDef{
						Attr: engine.Attribute{
							Name:    "b",
							Type:    types.New(types.T_int64, 0, 0, 0),
							Primary: false,
						},
					},
				},
			},
		)
		assert.Equal(t, txnengine.ErrExisted(false), resp.ErrExisted)
	}

	// get relations
	{
		resp := testRead[txnengine.GetRelationsResp](
			t, s, txnMeta,
			txnengine.OpGetRelations,
			txnengine.GetRelationsReq{
				DatabaseID: dbID,
			},
		)
		assert.Equal(t, 1, len(resp.Names))
		assert.Equal(t, "table", resp.Names[0])
	}

	// open relation
	var relID string
	{
		resp := testRead[txnengine.OpenRelationResp](
			t, s, txnMeta,
			txnengine.OpOpenRelation,
			txnengine.OpenRelationReq{
				DatabaseID: dbID,
				Name:       "table",
			},
		)
		assert.Equal(t, "", resp.ErrNotFound.Name)
		assert.NotNil(t, resp.ID)
		relID = resp.ID
		assert.Equal(t, txnengine.RelationTable, resp.Type)
	}
	_ = relID

	defer func() {
		// delete relation
		{
			resp := testWrite[txnengine.DeleteRelationResp](
				t, s, txnMeta,
				txnengine.OpDeleteRelation,
				txnengine.DeleteRelationReq{
					DatabaseID: dbID,
					Name:       "table",
				},
			)
			assert.Equal(t, "", resp.ErrNotFound.Name)
		}
		{
			resp := testRead[txnengine.GetRelationsResp](
				t, s, txnMeta,
				txnengine.OpGetRelations,
				txnengine.GetRelationsReq{
					DatabaseID: dbID,
				},
			)
			assert.Equal(t, 0, len(resp.Names))
		}
	}()

	//TODO AddTableDef
	//TODO DelTableDef
	//TODO GetPrimaryKeys
	//TODO GetTableDefs
	//TODO Write
	//TODO Read

}

func testRead[
	Resp any,
	Req any,
](
	t *testing.T,
	s *Storage,
	txnMeta txn.TxnMeta,
	op uint32,
	req Req,
) (
	resp Resp,
) {

	buf := new(bytes.Buffer)
	err := gob.NewEncoder(buf).Encode(req)
	assert.Nil(t, err)

	res, err := s.Read(context.TODO(), txnMeta, op, buf.Bytes())
	assert.Nil(t, err)
	data, err := res.Read()
	assert.Nil(t, err)

	err = gob.NewDecoder(bytes.NewReader(data)).Decode(&resp)
	assert.Nil(t, err)

	return
}

func testWrite[
	Resp any,
	Req any,
](
	t *testing.T,
	s *Storage,
	txnMeta txn.TxnMeta,
	op uint32,
	req Req,
) (
	resp Resp,
) {

	buf := new(bytes.Buffer)
	err := gob.NewEncoder(buf).Encode(req)
	assert.Nil(t, err)

	data, err := s.Write(context.TODO(), txnMeta, op, buf.Bytes())
	assert.Nil(t, err)

	err = gob.NewDecoder(bytes.NewReader(data)).Decode(&resp)
	assert.Nil(t, err)

	return
}
