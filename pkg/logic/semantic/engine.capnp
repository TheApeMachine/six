@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("semantic");
$Go.import("github.com/theapemachine/six/pkg/logic/semantic");

interface Engine {
  prompt @0 (msg :Text) -> (result :Text);
}
