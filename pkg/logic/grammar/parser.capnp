@0xad058c9d70413d68;

using Go = import "/go.capnp";
$Go.package("grammar");
$Go.import("github.com/theapemachine/six/pkg/logic/grammar");

interface Parser {
  prompt @0 (msg :Text) -> (result :Text);
}
