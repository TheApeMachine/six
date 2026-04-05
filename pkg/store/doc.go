/*
Package store implements a labeled token-trie (markovtrie) for online sequence
storage and an in-process Kadabra DHT that publishes and retrieves sequence
records across a peer set.

MarkovTrie is the substrate for the behaviors exercised by the browser
CognitiveModel demo ( train, Experience / surprise-modulated plasticity,
Classify, SurprisalSeries, InterpolatedProbabilities, NextProbabilities,
Generate with repetition damping, BeamSearch, ExtractPatterns, PosteriorsOverTime,
ReplayOne / REM-style consolidation, episodic tail blending, SemanticEquivalent /
AttentionContext, and ClassifyDetailed for contrastive token traces ). Use
WithWordTokensOnly when token streams should match the demo’s underscore-delimited
words instead of treating "_" as its own trie symbol. Kadabra does not embed
those algorithms: it hashes (sequence, label) keys, replicates SequenceRecord
values, and routes lookups; the trie lives in each host’s markovtrie.Store.
*/
package store
