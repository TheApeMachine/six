/*
Package store implements a labeled token-trie for online sequence storage and
an in-process Kadabra-style DHT node that can publish and retrieve those
sequences across a peer set.

The sequence layer keeps lazy-decayed per-label counts at each node, learns
local word co-occurrence, scores next tokens by interpolating over suffix
contexts, supports token-level fuzzy lookup, beam search, surprisal traces,
posterior traces, repeated-symbol extraction, and replay-based self-training
for novel high-confidence generations.

The DHT layer wraps the store in a 64-bit XOR keyspace with k-buckets, closest
peer lookup, replicated record placement, iterative retrieval, and per-bucket
adaptive peer exploration guided by observed RTT and a configurable minimum
exploration latency floor.
*/
package store
