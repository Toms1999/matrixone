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

package txnengine

import (
	"context"
	"strings"
	"time"

	logservicepb "github.com/matrixorigin/matrixone/pkg/pb/logservice"
	"github.com/matrixorigin/matrixone/pkg/txn/client"
	"github.com/matrixorigin/matrixone/pkg/vm/engine"
)

// Engine is an engine.Engine impl
type Engine struct {
	shardPolicy       ShardPolicy
	getClusterDetails GetClusterDetailsFunc
}

type GetClusterDetailsFunc = func() (logservicepb.ClusterDetails, error)

func New(
	ctx context.Context,
	shardPolicy ShardPolicy,
	getClusterDetails GetClusterDetailsFunc,
) *Engine {

	engine := &Engine{
		shardPolicy:       shardPolicy,
		getClusterDetails: getClusterDetails,
	}

	return engine
}

var _ engine.Engine = new(Engine)

func (e *Engine) Create(ctx context.Context, dbName string, txnOperator client.TxnOperator) error {

	_, err := engine.DoTxnRequest[CreateDatabaseResp](
		ctx,
		e,
		txnOperator.Write,
		e.allNodesShards,
		OpCreateDatabase,
		CreateDatabaseReq{
			Name: strings.ToLower(dbName),
		},
	)
	if err != nil {
		return err
	}

	return nil
}

func (e *Engine) Database(ctx context.Context, dbName string, txnOperator client.TxnOperator) (engine.Database, error) {

	resps, err := engine.DoTxnRequest[OpenDatabaseResp](
		ctx,
		e,
		txnOperator.Read,
		e.firstNodeShard,
		OpOpenDatabase,
		OpenDatabaseReq{
			Name: strings.ToLower(dbName),
		},
	)
	if err != nil {
		return nil, err
	}

	resp := resps[0]

	db := &Database{
		engine:      e,
		txnOperator: txnOperator,
		id:          resp.ID,
	}

	return db, nil
}

func (e *Engine) Databases(ctx context.Context, txnOperator client.TxnOperator) ([]string, error) {

	resps, err := engine.DoTxnRequest[GetDatabasesResp](
		ctx,
		e,
		txnOperator.Read,
		e.firstNodeShard,
		OpGetDatabases,
		GetDatabasesReq{},
	)
	if err != nil {
		return nil, err
	}

	var dbNames []string
	for _, resp := range resps {
		dbNames = append(dbNames, resp.Names...)
	}

	return dbNames, nil
}

func (e *Engine) Delete(ctx context.Context, dbName string, txnOperator client.TxnOperator) error {

	_, err := engine.DoTxnRequest[DeleteDatabaseResp](
		ctx,
		e,
		txnOperator.Write,
		e.allNodesShards,
		OpDeleteDatabase,
		DeleteDatabaseReq{
			Name: strings.ToLower(dbName),
		},
	)
	if err != nil {
		return err
	}

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
