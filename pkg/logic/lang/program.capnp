@0xad058c9d70413d87;

using Go = import "/go.capnp";
$Go.package("lang");
$Go.import("github.com/theapemachine/six/pkg/logic/lang");

using import "./primitive/value.capnp".Value;

struct Program {
  values @0 :List(Value);
}

interface Evaluator {
  write  @0 (seed :List(Value)) -> stream;
  done   @1 ();
}
