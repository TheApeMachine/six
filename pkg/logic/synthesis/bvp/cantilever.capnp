@0xad058c9d70413d6a;

using Go = import "/go.capnp";
$Go.package("bvp");
$Go.import("github.com/theapemachine/six/pkg/logic/synthesis/bvp");

interface Cantilever {
  prompt @0 (msg :Text) -> (result :Text);
}
