package vm

import (
	"fmt"

	capnp "capnproto.org/go/capnp/v3"
	"github.com/gomlx/gemma/transformers"
	"github.com/gomlx/gemma/trees"
	"github.com/gomlx/gomlx/backends"
	. "github.com/gomlx/gomlx/graph"
	gomlxctx "github.com/gomlx/gomlx/ml/context"
	"github.com/gomlx/gomlx/types/shapes"
	"github.com/gomlx/gomlx/types/tensors"
	"github.com/gomlx/gopjrt/dtypes"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/logic/substrate"
	"github.com/theapemachine/six/pkg/store/data"
	"github.com/theapemachine/six/pkg/store/lsm"
	"github.com/theapemachine/six/pkg/system/process/tokenizer"
)

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
	}

	for _, opt := range opts {
		opt(layer)
	}

	return layer
}

/*
IngestContext populates the substrate with context text via the Machine's
standard ingest pipeline (Tokenizer → SpatialIndex → Semantic). Call this
before generation to load the "memory" the transformer will draw from.
*/
func (tl *TranslationLayer) IngestContext(text string) error {
	return tl.machine.Ingest(text)
}

/*
QuerySubstrate runs the substrate pipeline (Tokenizer → SpatialIndex.Lookup →
Graph.Prompt → SpatialIndex.Decode) and returns the raw readout byte sequences.
This is the same pipeline as Machine.Prompt but returns the intermediate results.
*/
func (tl *TranslationLayer) QuerySubstrate(query []byte) ([][]byte, error) {
	ctx := tl.machine.ctx

	tokFuture, tokRelease := tl.machine.booter.tok.Generate(
		ctx, func(p tokenizer.Universal_generate_Params) error {
			return p.SetData(query)
		},
	)
	defer tokRelease()

	tokResult := errnie.SafeMust(func() (tokenizer.Universal_generate_Results, error) {
		return tokFuture.Struct()
	})

	keyList := errnie.SafeMust(func() (capnp.UInt64List, error) {
		return tokResult.Keys()
	})

	keys := keyListToSlice(keyList)
	promptValues := data.CompileObservableSequenceValues(keys)

	if len(promptValues) == 0 {
		return nil, nil
	}

	lookupFuture, lookupRelease := tl.machine.booter.spatialIndex.Lookup(ctx, func(
		p lsm.SpatialIndex_lookup_Params,
	) error {
		valueList, err := valueListFromSlice(p.Segment(), promptValues)
		if err != nil {
			return err
		}

		errnie.MustVoid(p.SetValues(valueList))

		return nil
	})
	defer lookupRelease()

	lookupResult := errnie.SafeMust(func() (lsm.SpatialIndex_lookup_Results, error) {
		return lookupFuture.Struct()
	})

	paths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return lookupResult.Paths()
	})

	metaPaths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return lookupResult.MetaPaths()
	})

	tl.machine.enrich(ctx, string(query), paths)

	graphFuture, graphRelease := tl.machine.booter.graph.Prompt(ctx, func(
		p substrate.Graph_prompt_Params,
	) error {
		errnie.MustVoid(p.SetPaths(paths))
		return p.SetMetaPaths(metaPaths)
	})
	defer graphRelease()

	graphResult := errnie.SafeMust(func() (substrate.Graph_prompt_Results, error) {
		return graphFuture.Struct()
	})

	resultPaths := errnie.SafeMust(func() (capnp.PointerList, error) {
		return graphResult.Result()
	})

	decodeFuture, decodeRelease := tl.machine.booter.spatialIndex.Decode(ctx, func(
		p lsm.SpatialIndex_decode_Params,
	) error {
		return p.SetValues(resultPaths)
	})
	defer decodeRelease()

	decodeResult := errnie.SafeMust(func() (lsm.SpatialIndex_decode_Results, error) {
		return decodeFuture.Struct()
	})

	seqList := errnie.SafeMust(func() (capnp.DataList, error) {
		return decodeResult.Sequences()
	})

	results := make([][]byte, seqList.Len())

	for i := range seqList.Len() {
		raw := errnie.SafeMust(func() ([]byte, error) {
			return seqList.At(i)
		})

		results[i] = make([]byte, len(raw))
		copy(results[i], raw)
	}

	return results, nil
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

	maskTensor := tensors.FromFlatDataAndDimensions(mask, 1, nTokens, config.MaxCacheLength)

	prefillExec := gomlxctx.NewExec(
		backend,
		ctx.Reuse(),
		func(mlCtx *gomlxctx.Context, inputs []*Node) []*Node {
			tokens := inputs[0]
			pos := inputs[1]
			attnMask := inputs[2]

			cacheTree := trees.FromValuesAndTree(
				inputs[3:3+cache.Data.NumLeaves()],
				trees.Map(cache.Data, func(_ trees.Path, _ *tensors.Tensor) struct{} { return struct{}{} }),
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
	x *Node,
	substrateTokens *Node,
) *Node {
	g := x.Graph()
	dtype := x.DType()
	embedDim := config.EmbedDim
	headDim := config.HeadDim
	numHeads := config.NumHeads

	normalized := transformers.RMSNorm(ctx.In("substrate_norm"), x)

	queryWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("q_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	query := Einsum("BTD,NHD->BTNH", normalized, queryWeights)

	embedTable := ctx.Reuse().In("model").In("embedder").
		VariableWithShape("input_embedding", shapes.Make(dtypes.BFloat16, config.VocabularySize, config.EmbedDim)).
		ValueGraph(g)

	substrateEmbed := Gather(embedTable, ExpandAxes(substrateTokens, -1))

	keyWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("k_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	valueWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("v_proj", shapes.Make(dtype, numHeads, embedDim, headDim)).
		ValueGraph(g)

	keys := Einsum("BSD,NHD->BSNH", substrateEmbed, keyWeights)
	values := Einsum("BSD,NHD->BSNH", substrateEmbed, valueWeights)

	logits := Einsum("BTNH,BSNH->BTNS", query, keys)
	scale := 1.0 / float64(headDim)
	logits = MulScalar(logits, scale)
	weights := Softmax(logits, -1)

	attended := Einsum("BTNS,BSNH->BTNH", weights, values)

	outWeights := ctx.In("substrate_cross_attn").
		VariableWithShape("o_proj", shapes.Make(dtype, numHeads, headDim, embedDim)).
		ValueGraph(g)

	output := Einsum("BTNH,NHD->BTD", attended, outWeights)

	return Add(x, output)
}

/*
GemmaWithSubstrate is a modified transformer forward pass that injects
substrate readout at configured layers. It re-uses Gemma's weights and
cache but adds substrate cross-attention at the specified injection points.
*/
func (tl *TranslationLayer) GemmaWithSubstrate(
	ctx *gomlxctx.Context,
	config *transformers.Config,
	currentTokens, currentPositions *Node,
	cache *trees.Tree[*Node],
	cacheAttentionMask *Node,
	substrateTokens *Node,
) *Node {
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

		x = Identity(x)
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
