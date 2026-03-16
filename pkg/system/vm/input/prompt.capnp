@0xad058c9d70413d70;

using Go = import "/go.capnp";
$Go.package("input");
$Go.import("github.com/theapemachine/six/pkg/system/vm/input");

using import "../../../store/data/value.capnp".Value;

interface Prompter {
  generate @0 (msg :Text) -> (data :Data);
  done     @1 ();
}
