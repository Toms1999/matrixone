// Copyright 2021 - 2022 Matrix Origin
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

package logservice

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLogRecord(t *testing.T) {
	r := LogRecord{
		Data: make([]byte, 32),
	}
	assert.Equal(t, 32-HeaderSize-8, len(r.Payload()))
	r.ResizePayload(2)
	assert.Equal(t, HeaderSize+8+2, len(r.Data))
	assert.Equal(t, 2, len(r.Payload()))
}

func TestCNStateUpdate(t *testing.T) {
	state := CNState{Stores: map[string]CNStoreInfo{}}

	hb1 := CNStoreHeartbeat{UUID: "cn-a", ServiceAddress: "addr-a"}
	tick1 := uint64(100)

	state.Update(hb1, tick1)
	assert.Equal(t, state.Stores[hb1.UUID], CNStoreInfo{
		Tick:           tick1,
		ServiceAddress: hb1.ServiceAddress,
	})

	hb2 := CNStoreHeartbeat{UUID: "cn-b", ServiceAddress: "addr-b"}
	tick2 := uint64(200)

	state.Update(hb2, tick2)
	assert.Equal(t, state.Stores[hb2.UUID], CNStoreInfo{
		Tick:           tick2,
		ServiceAddress: hb2.ServiceAddress,
	})

	hb3 := CNStoreHeartbeat{UUID: "cn-a", ServiceAddress: "addr-a"}
	tick3 := uint64(300)

	state.Update(hb3, tick3)
	assert.Equal(t, state.Stores[hb3.UUID], CNStoreInfo{
		Tick:           tick3,
		ServiceAddress: hb3.ServiceAddress,
	})
}

func TestDNStateUpdate(t *testing.T) {
	state := DNState{Stores: map[string]DNStoreInfo{}}

	hb1 := DNStoreHeartbeat{
		UUID:           "dn-a",
		ServiceAddress: "addr-a",
		Shards: []DNShardInfo{{
			ShardID:   1,
			ReplicaID: 1,
		}},
	}
	tick1 := uint64(100)

	state.Update(hb1, tick1)
	assert.Equal(t, state.Stores["dn-a"], DNStoreInfo{
		Tick:           tick1,
		ServiceAddress: hb1.ServiceAddress,
		Shards:         hb1.Shards,
	})

	hb2 := DNStoreHeartbeat{
		UUID:           "dn-a",
		ServiceAddress: "addr-a",
		Shards: []DNShardInfo{
			{ShardID: 1, ReplicaID: 1},
			{ShardID: 2, ReplicaID: 1}},
	}
	tick2 := uint64(200)

	state.Update(hb2, tick2)
	assert.Equal(t, state.Stores[hb2.UUID], DNStoreInfo{
		Tick:           tick2,
		ServiceAddress: hb2.ServiceAddress,
		Shards:         hb2.Shards,
	})
}

func TestLogStateUpdateStores(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("The code did not panic")
		}
	}()

	state := LogState{
		Shards: map[uint64]LogShardInfo{},
		Stores: map[string]LogStoreInfo{},
	}

	hb1 := LogStoreHeartbeat{
		UUID:           "log-a",
		RaftAddress:    "raft-a",
		ServiceAddress: "addr-a",
		GossipAddress:  "gossip-a",
		Replicas: []LogReplicaInfo{{
			LogShardInfo: LogShardInfo{
				ShardID:  1,
				Replicas: map[uint64]string{1: "log-a"},
				Epoch:    1,
				LeaderID: 1,
				Term:     1,
			},
			ReplicaID: 1,
		}},
	}
	tick1 := uint64(100)
	state.Update(hb1, tick1)
	assert.Equal(t, state.Stores[hb1.UUID], LogStoreInfo{
		Tick:           tick1,
		RaftAddress:    hb1.RaftAddress,
		ServiceAddress: hb1.ServiceAddress,
		GossipAddress:  hb1.GossipAddress,
		Replicas:       hb1.Replicas,
	})

	hb2 := LogStoreHeartbeat{
		UUID:           "log-a",
		RaftAddress:    "raft-a",
		ServiceAddress: "addr-a",
		GossipAddress:  "gossip-a",
		Replicas: []LogReplicaInfo{{
			LogShardInfo: LogShardInfo{
				ShardID:  1,
				Replicas: map[uint64]string{1: "log-a", 2: "log-b"},
				Epoch:    2,
				LeaderID: 1,
				Term:     2,
			},
			ReplicaID: 1,
		}},
	}
	tick2 := uint64(200)
	state.Update(hb2, tick2)
	assert.Equal(t, state.Stores[hb2.UUID], LogStoreInfo{
		Tick:           tick2,
		RaftAddress:    hb2.RaftAddress,
		ServiceAddress: hb2.ServiceAddress,
		GossipAddress:  hb2.GossipAddress,
		Replicas:       hb2.Replicas,
	})

	hb3 := LogStoreHeartbeat{
		UUID:           "log-a",
		RaftAddress:    "raft-a",
		ServiceAddress: "addr-a",
		GossipAddress:  "gossip-a",
		Replicas: []LogReplicaInfo{{
			LogShardInfo: LogShardInfo{
				ShardID:  1,
				Replicas: map[uint64]string{1: "log-a"},
				Epoch:    2,
				LeaderID: 1,
				Term:     2,
			},
			ReplicaID: 1,
		}},
	}
	tick3 := uint64(200)

	// should panic()
	state.Update(hb3, tick3)
}

func TestLogString(t *testing.T) {
	cases := []struct {
		desc string

		command  ScheduleCommand
		expected string
	}{
		{
			desc: "add log replica",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:       "storeB",
						ShardID:    1,
						ReplicaID:  4,
						Epoch:      1,
						LogShardID: 0,
					},
					ChangeType:     AddReplica,
					InitialMembers: nil,
				},
				ServiceType:   LogService,
				ShutdownStore: nil,
			},
			expected: "L/Add storeA storeB:1:4:1",
		},
		{
			desc: "remove log replica",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:       "storeB",
						ShardID:    1,
						ReplicaID:  4,
						Epoch:      1,
						LogShardID: 0,
					},
					ChangeType:     RemoveReplica,
					InitialMembers: nil,
				},
				ServiceType:   LogService,
				ShutdownStore: nil,
			},
			expected: "L/Remove storeA storeB:1:4:1",
		},
		{
			desc: "remove log replica",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:       "storeA",
						ShardID:    1,
						ReplicaID:  4,
						Epoch:      1,
						LogShardID: 0,
					},
					ChangeType:     StartReplica,
					InitialMembers: nil,
				},
				ServiceType:   LogService,
				ShutdownStore: nil,
			},
			expected: "L/Start storeA storeA:1:4:1",
		},
		{
			desc: "remove log replica",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:       "storeA",
						ShardID:    1,
						ReplicaID:  4,
						Epoch:      1,
						LogShardID: 0,
					},
					ChangeType:     StartReplica,
					InitialMembers: nil,
				},
				ServiceType:   DnService,
				ShutdownStore: nil,
			},
			expected: "D/Start storeA storeA:1:4:1",
		},
		{
			desc: "remove log replica",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ServiceType:   LogService,
				ShutdownStore: &ShutdownStore{
					StoreID: "storeA",
				},
			},
			expected: "L/shutdown storeA",
		},
		{
			desc: "kill zombie",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: false,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:    "storeA",
						ShardID: 1,
					},
					ChangeType: KillZombie,
				},
				ServiceType: LogService,
			},
			expected: "L/Kill storeA storeA:1:0:0",
		},
		{
			desc: "bootstrapping",
			command: ScheduleCommand{
				UUID:          "storeA",
				Bootstrapping: true,
				ConfigChange: &ConfigChange{
					Replica: Replica{
						UUID:      "storeA",
						ShardID:   1,
						ReplicaID: 1,
					},
					ChangeType:     StartReplica,
					InitialMembers: map[uint64]string{1: "storeA123", 2: "storeB", 3: "storeC"},
				},
				ServiceType: LogService,
			},
			expected: "L/Start storeA storeA:1:1:0 [1:storeA 2:storeB 3:storeC]",
		},
	}

	for _, c := range cases {
		output := c.command.LogString()
		assert.Equal(t, c.expected, output)
	}
}
