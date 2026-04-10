/*
Canonical event kind ids for viz binary wire format (pkg/viz/wire.go v1).
Keep in sync with pkg/viz/static/modules/event_kinds.js for the embedded static viewer.
*/

export const EK = {
    NodeCreated: 0, NodeUpdated: 1, NodeRemoved: 2,
    PeerAdded: 3, PeerRemoved: 4, PeerLatency: 5,
    ValuePublished: 6, ValueReplicated: 7,
    GossipSent: 8, GossipReceived: 9,
    FieldDigest: 10, EigenmodeDetected: 11, FieldPressure: 12,
    TrieInsert: 13, TrieDecay: 14, TriePrune: 15,
    TriePredict: 16, TrieClassify: 17, TrieExperience: 18,
    PoolSchedule: 19, PoolComplete: 20,
    AdaptiveUpdate: 21,
    TrieCoupling: 22, TrieMode: 23, TriePressure: 24, TrieSignal: 25,
    BeamCollect: 26, BeamCompose: 27, BeamBreak: 28, BeamConverge: 29,
    Prompt: 30, PromptResult: 31,
    TrieGraphSnapshot: 32,
    CompilerCompile: 33,
    ALUDispatch: 34,
    FinalizerRun: 35,
    DatasetRead: 36,
    TokenizerChunk: 37,
    TokenizerEmit: 38,
    QueueSubmit: 39,
} as const;

export const KIND_NAMES: Record<number, string> = Object.fromEntries(
    Object.entries(EK).map(([key, value]) => [value, key]),
);
