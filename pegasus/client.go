// Copyright (c) 2017, Xiaomi, Inc.  All rights reserved.
// This source code is licensed under the Apache License Version 2.0, which
// can be found in the LICENSE file in the root directory of this source tree.

package pegasus

import (
	"context"
	"sync"

	"github.com/zljohn-ux/pegasus-go-client/pegalog"
	"github.com/zljohn-ux/pegasus-go-client/session"
)

// Client manages the client sessions to the pegasus cluster specified by `Config`.
// In order to reuse the previous connections, it's recommended to use one singleton
// client in your program. The operations upon a client instance are thread-safe.
type Client interface {
	Close() error

	// Open the specific pegasus table. If the table was opened before,
	// it will reuse the previous connection to the table.
	OpenTable(ctx context.Context, tableName string) (TableConnector, error)
}

type pegasusClient struct {
	tables map[string]TableConnector

	// protect the access of tables
	mu sync.RWMutex

	metaMgr    *session.MetaManager
	replicaMgr *session.ReplicaManager
}

// NewClient creates a new instance of pegasus client.
func NewClient(cfg Config) Client {
	if len(cfg.MetaServers) == 0 {
		pegalog.GetLogger().Fatalln("pegasus-go-client: meta server list should not be empty")
		return nil
	}

	c := &pegasusClient{
		tables:     make(map[string]TableConnector),
		metaMgr:    session.NewMetaManager(cfg.MetaServers, session.NewNodeSession),
		replicaMgr: session.NewReplicaManager(session.NewNodeSession),
	}
	return c
}

func (p *pegasusClient) Close() error {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, table := range p.tables {
		if err := table.Close(); err != nil {
			return err
		}
	}

	if err := p.metaMgr.Close(); err != nil {
		pegalog.GetLogger().Fatalln("pegasus-go-client: unable to close metaMgr: ", err)
	}
	return p.replicaMgr.Close()
}

func (p *pegasusClient) OpenTable(ctx context.Context, tableName string) (TableConnector, error) {
	tb, err := func() (TableConnector, error) {
		// ensure only one goroutine is fetching the routing table.
		p.mu.Lock()
		defer p.mu.Unlock()

		if tb := p.findTable(tableName); tb != nil {
			return tb, nil
		}

		var tb TableConnector
		tb, err := ConnectTable(ctx, tableName, p.metaMgr, p.replicaMgr)
		if err != nil {
			return nil, err
		}
		p.tables[tableName] = tb

		return tb, nil
	}()
	return tb, WrapError(err, OpQueryConfig)
}

func (p *pegasusClient) findTable(tableName string) TableConnector {
	if tb, ok := p.tables[tableName]; ok {
		return tb
	}
	return nil
}
