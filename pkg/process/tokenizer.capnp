@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("process");
$Go.import("github.com/theapemachine/six/pkg/process");

using import "../data/chord.capnp".Chord;

struct Token {
  id     @0 :UInt64;
  chord  @1 :Chord;
}

interface Tokenizer {
  generate @0 (raw :Data) -> stream;
  done     @1 ();
}
