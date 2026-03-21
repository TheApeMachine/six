@0xad058c9d70423d67;

using Go = import "/go.capnp";
$Go.package("reader");
$Go.import("github.com/theapemachine/six/pkg/logic/reader");

using import "../lang/primitive/value.capnp".Value;

struct Head {
  input     @0 :Value;
  output    @1 :Value;
  operation @2 :Value;
}

interface Reader {
  write  @0 (value :Value) -> stream;
  done   @1 () -> (result :List(Value));
}
