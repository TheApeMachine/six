package markovtrie

import (
	"context"

	"github.com/theapemachine/six/pkg/core/numeric/geometry"
	"github.com/theapemachine/six/pkg/core/validate"
	"github.com/theapemachine/six/pkg/gossip"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Store is a trie that sits inside a Kadabra DHT node, and holds
a sequence of Values. Store acts as a field over the Values,
with a GF(257) phase field that is acts as a top-down feedback
mechanism.
*/
type Store struct {
	ctx      context.Context
	cancel   context.CancelFunc
	root     *Node
	ID       uint64
	conn     *gossip.Conn
	field    *geometry.Field
	Affinity []uint64
}

/*
Option configures a Store.
*/
type Option func(*Store)

/*
NewStore constructs a Store with token-level defaults.
*/
func NewStore(
	ctx context.Context,
	aff []uint64,
	options ...Option,
) (*Store, error) {
	ctx, cancel := context.WithCancel(ctx)

	store := &Store{
		ctx:      ctx,
		cancel:   cancel,
		Affinity: aff,
		conn:     gossip.NewConn(nil, nil),
		field:    geometry.NewField(geometry.Mod257),
	}

	for _, option := range options {
		option(store)
	}

	return store, validate.Require(map[string]any{
		"ctx":    store.ctx,
		"cancel": store.cancel,
		"conn":   store.conn,
		"field":  store.field,
	})
}

/*
Load inserts a Value into the trie. If a path already exists, the Value
lands at the existing branch; otherwise a new child is created.
*/
func (store *Store) Load(
	value primitive.Value,
	labels ...string,
) error {
	if store == nil {
		return nil
	}

	if store.root == nil {
		store.root = NewNode(value)
	}

	store.root.storeChild(
		value, NewNode(value),
	)

	return nil
}
