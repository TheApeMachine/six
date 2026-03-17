@0xd3c5d01f75666abd;

using Go = import "/go.capnp";
$Go.package("dmt");
$Go.import("github.com/theapemachine/six/pkg/store/dmt");

struct InsertPayload {
  key @0 :Data;
  value @1 :Data;
  merkleProof @2 :List(Data);
  term @3 :UInt64;
  logIndex @4 :UInt64;
}

struct SyncPayload {
  merkleRoot @0 :Data;
  entries @1 :List(SyncEntry);
  proofs @2 :List(MerkleProof);
  term @3 :UInt64;
  logIndex @4 :UInt64;
}

struct SyncEntry {
  key @0 :Data;
  value @1 :Data;
  term @2 :UInt64;
  index @3 :UInt64;
}

struct ProofPayload {
  key @0 :Data;
  value @1 :Data;
  proof @2 :List(Data);
}

struct MerkleProof {
  key @0 :Data;
  proof @1 :List(Data);
}

struct RecoverPayload {
  lastKnownMerkleRoot @0 :Data;
}

struct RequestVotePayload {
  term @0 :UInt64;
  candidateId @1 :Text;
  lastLogIndex @2 :UInt64;
  lastLogTerm @3 :UInt64;
}

struct HeartbeatPayload {
  term @0 :UInt64;
  leaderId @1 :Text;
}

interface RadixRPC {
  insert @0 (key :Data, value :Data, term :UInt64, logIndex :UInt64) -> (success :Bool, term :UInt64, logIndex :UInt64);
  sync @1 (merkleRoot :Data, term :UInt64, logIndex :UInt64) -> (diff :SyncPayload);
  recover @2 (lastKnownMerkleRoot :Data, lastTerm :UInt64, lastLogIndex :UInt64) -> (state :SyncPayload);
  requestVote @3 (term :UInt64, candidateId :Text, lastLogIndex :UInt64, lastLogTerm :UInt64) -> (term :UInt64, voteGranted :Bool);
  heartbeat @4 (term :UInt64, leaderId :Text) -> (term :UInt64, success :Bool);
}