@0xad058c9d70413d6c;

using Go = import "/go.capnp";
$Go.package("macro");
$Go.import("github.com/theapemachine/six/pkg/logic/synthesis/macro");

interface MacroIndex {
  prompt @0 (msg :Text) -> (result :Text);
}


