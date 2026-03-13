@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("process");
$Go.import("github.com/theapemachine/six/pkg/system/process");

using import "../../store/data/chord.capnp".Chord;

struct Token {
  id     @0 :UInt64;
  chord  @1 :Chord;
}

interface Tokenizer {
  generate @0 () -> stream;
  done     @1 ();
}
