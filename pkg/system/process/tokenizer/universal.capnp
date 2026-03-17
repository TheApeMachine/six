@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("tokenizer");
$Go.import("github.com/theapemachine/six/pkg/system/process/tokenizer");

interface Universal {
  write       @0 (data :UInt8) -> stream;
  done        @1 ();
  setDataset  @2 (corpus :List(Text)) -> ();
  feedback    @3 (overDiscriminated :Bool, underDiscriminated :Bool) -> ();
}
