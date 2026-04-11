/*
Package kadabra implements the affinity-routed DHT and the field layer that
sits above Markov tries.

Canonical flow (see README “Canonical ingest path”):

 1. Values are minted from byte streams via primitive.NewValue: incoming bytes
    Morton-pack into the token region until that region is full; additional
    bytes continue in linked segments.

 2. Affinity is derived from the token content (ComputeAffinityLSH) so routing
    fingerprints the ingested data.

 3. Publish stores the record on the local node: primary ingest blends toward
    meshLoad (blendMeshLoadCentroid) under the same Shannon cap as trie
    clusters; selectOrSpawnTrie picks or spawns a trie; Store.Load runs the
    trie.

 4. Trie centroids blend incoming affinities (selectOrSpawnTrie, Blended)
    until kadabra.shannonLimit popcount is reached, then spawnTrie may add a
    new trie. At node saturation, blendMeshLoadCentroid invokes onMeshExpand.

 5. Gossip.Digests exposes one Digest per local trie (affinity, signals,
    NodePhase). Field.refreshNodePhase aggregates trie-local GF(257) phases
    into a node GF(8191) vector; absorbed remote digests contribute to
    GF(65537) global phase — the layered substrate gossip is meant to exploit.
*/

package kadabra
