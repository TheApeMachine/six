@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("tokenizer");
$Go.import("github.com/theapemachine/six/pkg/system/process/tokenizer");

using import "../../../store/data/chord.capnp".Chord;
using import "../../../store/lsm/spatial_index.capnp".GraphEdge;

interface Universal {
  generate    @0 (data :Data) -> (edges :List(GraphEdge));
  done        @1 ();
  setDataset  @2 (corpus :List(Text)) -> ();
}
