package vision

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/provider/huggingface"
	"github.com/theapemachine/six/store"
	"github.com/theapemachine/six/tokenizer"
	"github.com/theapemachine/six/vm"
)

func TestReconstruction(t *testing.T) {
	Convey("Given a machine", t, func() {
		loader := vm.NewLoader(
			vm.LoaderWithStore(store.NewLSMSpatialIndex(1.0)),
			vm.LoaderWithTokenizer(tokenizer.NewUniversal(
				tokenizer.TokenizerWithDataset(
					huggingface.New(
						huggingface.DatasetWithRepo(
							"uoft-cs/cifar10",
						),
						huggingface.DatasetWithSamples(100),
						huggingface.DatasetWithTextColumn("image"),
					),
				),
			)),
		)
		
		machine := vm.NewMachine(
			vm.MachineWithLoader(loader),
		)

		machine.Start()
		loader.Holdout(50, 5)
		
		Convey("When reconstructing masked out halves of images", t, func() {
			for prompt := range loader.Prompts() {
				for res := range machine.Prompt(prompt) {
					So(res.Key, ShouldBeGreaterThan, 0)
				}		
			}
		})
	})	
}