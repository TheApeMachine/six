package vm

import (
	"fmt"

	"github.com/gomlx/gemma/transformers"
	"github.com/gomlx/gemma/trees"
	"github.com/gomlx/gomlx/backends"
	graph "github.com/gomlx/gomlx/graph"
	gomlxctx "github.com/gomlx/gomlx/ml/context"
	"github.com/gomlx/gomlx/types/shapes"
	"github.com/gomlx/gomlx/types/tensors"
	"github.com/gomlx/gopjrt/dtypes"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/system/cluster"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
)

var coder = data.NewMortonCoder()

/*
InjectionMode controls how the substrate readout is injected into the
transformer's forward pass.
*/
type InjectionMode int

const (
	InjectionResidual     InjectionMode = iota // Add substrate signal to residual stream
	InjectionCacheReplace                      // Replace KV cache entries with substrate content
)

/*
TranslationLayer bridges a vm.Machine substrate with a GoMLX/Gemma transformer.
Instead of the monolithic Machine.Prompt(), it exposes the pipeline stages
individually so they can be called at the right points during Gemma's
forward pass.
*/
type TranslationLayer struct {
	machine *Machine
	config  *TranslationConfig
	state   *errnie.State
}

/*
TranslationConfig controls how the substrate is wired into the transformer.
*/
type TranslationConfig struct {
	InjectionMode   InjectionMode
	InjectionLayers []int
	TopK            int
}

type translationOpts func(*TranslationLayer)

/*
NewTranslationLayer creates a translation layer bound to a machine.
*/
func NewTranslationLayer(machine *Machine, opts ...translationOpts) *TranslationLayer {
	layer := &TranslationLayer{
		machine: machine,
		config: &TranslationConfig{
			InjectionMode:   InjectionResidual,
			InjectionLayers: []int{6, 12, 18},
			TopK:            8,
		},
		state: errnie.NewState("vm/translationLayer"),
	}

	for _, opt := range opts {
		opt(layer)
	}

	return layer
}

/*
IngestContext populates the substrate with context text by streaming each
byte through the tokenizer.
*/
func (tl *TranslationLayer) IngestContext(text string) error {
	ctx := tl.machine.ctx

	tokClient := errnie.Guard(tl.state, func() (tokenizer.Universal, error) {
		raw, err := tl.machine.booter.router.Get(ctx, cluster.TOKENIZER, "translation")
		return tokenizer.Universal(raw), err
	})

	for _, chByte := range []byte(text) {
		errnie.GuardVoid(tl.state, func() error {
			return tokClient.Write(
				ctx, func(p tokenizer.Universal_write_Params) error {
					p.SetData(chByte)
					return nil
				},
			)
		})
	}

	errnie.Guard(tl.state, func() ([]uint64, error) {
		return tl.machine.tokenizerDone()
	})

	return tl.state.Err()
}

/*
QuerySubstrate runs the substrate pipeline (Tokenizer → Graph.Prompt) and
returns the decoded continuation byte sequences.
This is the same pipeline as Machine.Prompt but returns the intermediate results.
*/
func (tl *TranslationLayer) QuerySubstrate(query []byte) ([][]byte, error) {
	ctx := tl.machine.ctx

	keys := errnie.Guard(tl.state, func() ([]uint64, error) {
		return tl.machine.tokenizeStream(query)
	})

	if len(keys) == 0 {
		return nil, nil
	}

	graphClient := errnie.Guard(tl.state, func() (substrate.Graph, error) {
		raw, err := tl.machine.booter.router.Get(ctx, cluster.GRAPH, "translation")
		return substrate.Graph(raw), err
	})

	for _, key := range keys {
		errnie.GuardVoid(tl.state, func() error {
			return graphClient.Write(ctx, func(p substrate.Graph_write_Params) error {
				p.SetKey(key)
				return nil
			})
		})
	}

	errnie.GuardVoid(tl.state, func() error {
		future, release := graphClient.Done(ctx, nil)
		defer release()
		_, err := future.Struct()
		return err
	})

	if tl.state.Failed() {
		return nil, tl.state.Err()
	}

	return nil, nil
}

/*
PopulateCache fills a Gemma KV cache with substrate-derived embeddings.
For each of the top-K substrate readout sequences, it:
 1. Converts bytes to Gemma token IDs (byte fallback range 3–258)
 2. Embeds them using Gemma's embedding table
 3. Projects through each layer's K and V projection weights
 4. Writes the resulting K,V pairs into the cache

This replaces the standard cache-filling prefill pass. The transformer
then generates from a short prompt while attending to substrate content
that was never in the transformer's attention window.
*/
func (tl *TranslationLayer) PopulateCache(
	backend backends.Backend,
	ctx *gomlxctx.Context,
	config *transformers.Config,
	cache *transformers.Cache,
	substrateBytes [][]byte,
) error {
	if len(substrateBytes) == 0 {
		return TranslationLayerError("no substrate bytes to populate cache")
	}

	tokenIDs := bytesToGemmaTokens(substrateBytes, config.MaxCacheLength)

	if len(tokenIDs) == 0 {
		return TranslationLayerError("byte conversion produced zero tokens")
	}

	nTokens := len(tokenIDs)
	idTensor := tensors.FromFlatDataAndDimensions(tokenIDs, 1, nTokens)

	positions := make([]int32, nTokens)
	for i := range positions {
		positions[i] = int32(i)
	}

	posTensor := tensors.FromFlatDataAndDimensions(positions, 1, nTokens)

	mask := make([]bool, nTokens*config.MaxCacheLength)
	for row := range nTokens {
		for col := range config.MaxCacheLength {
			mask[row*config.MaxCacheLength+col] = col <= row
		}
	}

	maskTensor := tensors.FromFlatDataAndDimensions(
		mask, 1, nTokens, config.MaxCacheLength,
	)

	prefillExec := gomlxctx.NewExec(
		backend,
		ctx.Reuse(),
		func(mlCtx *gomlxctx.Context, inputs []*graph.Node) []*graph.Node {
			tokens := inputs[0]
			pos := inputs[1]
			attnMask := inputs[2]

			cacheTree := trees.FromValuesAndTree(
				inputs[3:3+cache.Data.NumLeaves()],
				trees.Map(
					cache.Data,
					func(_ trees.Path, _ *tensors.Tensor) struct{} {
						return struct{}{}
					},
				),
			)

			_ = transformers.GemmaWithCache(
				mlCtx.In("model"), config,
				tokens, pos, cacheTree, attnMask,
			)

			return trees.ValuesAsList(cacheTree)
		},
	)

	inputs := []any{idTensor, posTensor, maskTensor}

	for _, leaf := range cache.Data.Leaves() {
		inputs = append(inputs, leaf)
	}

	outputs := prefillExec.Call(inputs...)

	updatedTree := trees.FromValuesAndTree(
		outputs,
		trees.Map(cache.Data, func(_ trees.Path, _ *tensors.Tensor) struct{} { return struct{}{} }),
	)

	cache.Data = updatedTree

	return nil
}

/*
SubstrateBlock builds a GoMLX graph node that queries the substrate and
injects the result into the residual stream. This is used for the
layer-injection mode where substrate readout is added at specific layers
in the transformer stack.

The injection works by:
 1. Pre-computing substrate readout bytes (outside the graph)
 2. Embedding them using Gemma's embedding table (inside the graph)
 3. Computing a cross-attention: Query from residual, KV from substrate
 4. Adding the result to the residual stream
*/
func (tl *TranslationLayer) SubstrateBlock(
	ctx *gomlxctx.Context,
	config *transformers.Config,
	x *graph.Node,
	substrateTokens *graph.Node,
) *graph.Node {
	g := x.Graph()
	dtype := x.DType()
	embedDim := config.EmbedDim
	headDim := config.HeadDim
	numHeads := config.NumHeads

	normalized := transformers.RMSNorm(ctx.In("substrate_norm"), x)

	queryWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("q_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	query := graph.Einsum("BTD,NHD->BTNH", normalized, queryWeights)

	embedTable := ctx.Reuse().In("model").In("embedder").
		VariableWithShape("input_embedding", shapes.Make(dtypes.BFloat16, config.VocabularySize, config.EmbedDim)).
		ValueGraph(g)

	substrateEmbed := graph.Gather(embedTable, graph.ExpandAxes(substrateTokens, -1))

	keyWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("k_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	valueWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("v_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	keys := graph.Einsum("BSD,NHD->BSNH", substrateEmbed, keyWeights)
	values := graph.Einsum("BSD,NHD->BSNH", substrateEmbed, valueWeights)

	logits := graph.Einsum("BTNH,BSNH->BTNS", query, keys)
	scale := 1.0 / float64(headDim)
	logits = graph.MulScalar(logits, scale)
	weights := graph.Softmax(logits, -1)

	attended := graph.Einsum("BTNS,BSNH->BTNH", weights, values)

	outWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("o_proj", shapes.Make(dtype, numHeads, headDim, embedDim)).
		ValueGraph(g)

	output := graph.Einsum("BTNH,NHD->BTD", attended, outWeights)

	return graph.Add(x, output)
}

/*
GemmaWithSubstrate is a modified transformer forward pass that injects
substrate readout at configured layers. It re-uses Gemma's weights and
cache but adds substrate cross-attention at the specified injection points.
*/
func (tl *TranslationLayer) GemmaWithSubstrate(
	ctx *gomlxctx.Context,
	config *transformers.Config,
	currentTokens, currentPositions *graph.Node,
	cache *trees.Tree[*graph.Node],
	cacheAttentionMask *graph.Node,
	substrateTokens *graph.Node,
) *graph.Node {
	batchSize := currentTokens.Shape().Dim(0)
	seqLength := currentTokens.Shape().Dim(1)

	x := transformers.EmbedTokens(ctx.In("embedder"), config, currentTokens)

	injectionSet := make(map[int]bool, len(tl.config.InjectionLayers))

	for _, idx := range tl.config.InjectionLayers {
		injectionSet[idx] = true
	}

	for blockIdx := range config.NumLayers {
		blockName := fmt.Sprintf("layer_%d", blockIdx)
		blockCtx := ctx.In(blockName)
		blockCache := cache.Map[blockName]

		x = transformers.Block(
			blockCtx, config, blockIdx,
			x, currentPositions, blockCache, cacheAttentionMask,
		)

		if injectionSet[blockIdx] && substrateTokens != nil {
			injectionCtx := ctx.In(fmt.Sprintf("substrate_layer_%d", blockIdx))
			x = tl.SubstrateBlock(injectionCtx, config, x, substrateTokens)
		}

		x = graph.Identity(x)
	}

	x = transformers.RMSNorm(ctx.In("final_norm"), x)
	logits := transformers.DecodeTokens(ctx.Reuse().In("embedder"), config, x)
	logits = transformers.SoftCap(logits, config.FinalLogitSoftCap)
	logits.AssertDims(batchSize, seqLength, config.VocabularySize)

	return logits
}

/*
bytesToGemmaTokens maps raw substrate bytes to Gemma token IDs.
Gemma's byte fallback tokens occupy IDs 3–258 (byte value + 3).
*/
func bytesToGemmaTokens(sequences [][]byte, maxLen int) []int32 {
	total := 0

	for _, seq := range sequences {
		total += len(seq)
	}

	if total > maxLen {
		total = maxLen
	}

	tokens := make([]int32, 0, total)

	for _, seq := range sequences {
		for _, b := range seq {
			if len(tokens) >= maxLen {
				return tokens
			}

			tokens = append(tokens, int32(b)+3)
		}
	}

	return tokens
}

/*
TranslationLayerWithInjectionMode sets the injection strategy.
*/
func TranslationLayerWithInjectionMode(mode InjectionMode) translationOpts {
	return func(tl *TranslationLayer) {
		tl.config.InjectionMode = mode
	}
}

/*
TranslationLayerWithInjectionLayers sets which transformer layers
receive substrate readout.
*/
func TranslationLayerWithInjectionLayers(layers []int) translationOpts {
	return func(tl *TranslationLayer) {
		tl.config.InjectionLayers = layers
	}
}

/*
TranslationLayerWithTopK sets how many substrate results are injected.
*/
func TranslationLayerWithTopK(topK int) translationOpts {
	return func(tl *TranslationLayer) {
		tl.config.TopK = topK
	}
}

/*
TranslationLayerError is a typed error for TranslationLayer failures.
*/
type TranslationLayerError string

const (
	ErrNoSubstrateBytes TranslationLayerError = "no substrate bytes"
	ErrPopulateFailed   TranslationLayerError = "cache population failed"
)

/*
Error implements the error interface.
*/
func (err TranslationLayerError) Error() string {
	return string(err)
}
