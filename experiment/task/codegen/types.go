package codegen

// SpanSolverEntry holds one prompt's solver result.
type SpanSolverEntry struct {
	Desc            string    `json:"desc"`
	Prefix          string    `json:"prefix"`
	Generated       string    `json:"generated"`
	Converged       bool      `json:"converged"`
	Iterations      int       `json:"iterations"`
	HasReturn       bool      `json:"has_return"`
	HasColon        bool      `json:"has_colon"`
	UniqueRatio     float64   `json:"unique_ratio"`
	PrefixRelevance float64   `json:"prefix_relevance"`
	TopSpans        []string  `json:"top_spans"`
	TopScores       []float64 `json:"top_scores"`
}

// SpanSolverResult holds the full span solver experiment results.
type SpanSolverResult struct {
	SpanLength       int               `json:"span_length"`
	TopK             int               `json:"top_k"`
	RefineIterations int               `json:"refine_iterations"`
	DialAngles       int               `json:"dial_angles"`
	TotalSpans       int               `json:"total_spans"`
	Entries          []SpanSolverEntry `json:"entries"`
	ConvergedCount   int               `json:"converged_count"`
	ReturnCount      int               `json:"return_count"`
	ColonCount       int               `json:"colon_count"`
	MeanUniqueRatio  float64           `json:"mean_unique_ratio"`
	MeanRelevance    float64           `json:"mean_relevance"`
}

// SpanCandidate holds one candidate span's scoring breakdown.
type SpanCandidate struct {
	Rank        int     `json:"rank"`
	Text        string  `json:"text"`
	Length      int     `json:"length"`
	SimScore    float64 `json:"sim_score"`
	PrefixOvl   float64 `json:"prefix_ovl"`
	StructBonus float64 `json:"struct_bonus"`
	Total       float64 `json:"total"`
	SourceIdx   int     `json:"source_idx"`
}

// SpanRankingEntry holds one prompt's span ranking result.
type SpanRankingEntry struct {
	Desc           string          `json:"desc"`
	Prefix         string          `json:"prefix"`
	WinnerText     string          `json:"winner_text"`
	WinnerLength   int             `json:"winner_length"`
	WinnerSim      float64         `json:"winner_sim"`
	WinnerTotal    float64         `json:"winner_total"`
	HasReturn      bool            `json:"has_return"`
	HasColon       bool            `json:"has_colon"`
	HasIndent      bool            `json:"has_indent"`
	IdentReuse     int             `json:"ident_reuse"`
	ExactCorpus    bool            `json:"exact_corpus"`
	TopCandidates  []SpanCandidate `json:"top_candidates"`
	TotalRetrieved int             `json:"total_retrieved"`
}

// SpanRankingResult holds the full span ranking experiment results.
type SpanRankingResult struct {
	TotalSpans    int                `json:"total_spans"`
	SpanLengths   []int              `json:"span_lengths"`
	DialAngles    int                `json:"dial_angles"`
	TopK          int                `json:"top_k"`
	Entries       []SpanRankingEntry `json:"entries"`
	ExactCount    int                `json:"exact_count"`
	ReturnCount   int                `json:"return_count"`
	ColonCount    int                `json:"colon_count"`
	IndentCount   int                `json:"indent_count"`
	MeanWinnerSim float64            `json:"mean_winner_sim"`
}

// ChainedSpan holds one step in a span chain.
type ChainedSpan struct {
	Step       int     `json:"step"`
	Text       string  `json:"text"`
	Length     int     `json:"length"`
	SimScore   float64 `json:"sim_score"`
	SourceIdx  int     `json:"source_idx"`
	ExactMatch bool    `json:"exact_match"`
	Continuity bool    `json:"continuity"`
}

// SpanChainingEntry holds one prompt's chaining result.
type SpanChainingEntry struct {
	Desc         string        `json:"desc"`
	Prefix       string        `json:"prefix"`
	FullText     string        `json:"full_text"`
	Chain        []ChainedSpan `json:"chain"`
	ChainLength  int           `json:"chain_length"`
	HasReturn    bool          `json:"has_return"`
	HasColon     bool          `json:"has_colon"`
	HasLoop      bool          `json:"has_loop"`
	LooksValid   bool          `json:"looks_valid"`
	SingleSource bool          `json:"single_source"`
	SourceCount  int           `json:"source_count"`
}

// SpanChainingResult holds the full span chaining experiment results.
type SpanChainingResult struct {
	TotalSpans     int                 `json:"total_spans"`
	MaxChains      int                 `json:"max_chains"`
	Entries        []SpanChainingEntry `json:"entries"`
	ValidCount     int                 `json:"valid_count"`
	ReturnCount    int                 `json:"return_count"`
	LoopCount      int                 `json:"loop_count"`
	SingleSrcCount int                 `json:"single_src_count"`
}

// OverlapChainStep holds one step in an overlap-aware chain.
type OverlapChainStep struct {
	Step       int     `json:"step"`
	SpanText   string  `json:"span_text"`
	NewText    string  `json:"new_text"`
	NewTokens  int     `json:"new_tokens"`
	Overlap    int     `json:"overlap"`
	SimScore   float64 `json:"sim_score"`
	SourceIdx  int     `json:"source_idx"`
	ExactMatch bool    `json:"exact_match"`
}

// OverlapChainingEntry holds one prompt's overlap chaining result.
type OverlapChainingEntry struct {
	Desc         string             `json:"desc"`
	Prefix       string             `json:"prefix"`
	FullText     string             `json:"full_text"`
	Chain        []OverlapChainStep `json:"chain"`
	ChainLength  int                `json:"chain_length"`
	TotalNew     int                `json:"total_new"`
	HasReturn    bool               `json:"has_return"`
	HasColon     bool               `json:"has_colon"`
	HasLoop      bool               `json:"has_loop"`
	LooksValid   bool               `json:"looks_valid"`
	SingleSource bool               `json:"single_source"`
	SourceCount  int                `json:"source_count"`
}

// OverlapChainingResult holds the full overlap chaining experiment results.
type OverlapChainingResult struct {
	TotalSpans     int                    `json:"total_spans"`
	MaxChains      int                    `json:"max_chains"`
	MinNewTokens   int                    `json:"min_new_tokens"`
	Entries        []OverlapChainingEntry `json:"entries"`
	ValidCount     int                    `json:"valid_count"`
	ReturnCount    int                    `json:"return_count"`
	LoopCount      int                    `json:"loop_count"`
	SingleSrcCount int                    `json:"single_src_count"`
	MeanNewTokens  float64                `json:"mean_new_tokens"`
}

// LongGenStep holds one step in a long generation chain.
type LongGenStep struct {
	Step      int     `json:"step"`
	SpanText  string  `json:"span_text"`
	NewText   string  `json:"new_text"`
	NewTokens int     `json:"new_tokens"`
	Overlap   int     `json:"overlap"`
	SimScore  float64 `json:"sim_score"`
	SourceIdx int     `json:"source_idx"`
}

// LongGenEntry holds one prompt's long generation result.
type LongGenEntry struct {
	Desc           string        `json:"desc"`
	Prefix         string        `json:"prefix"`
	FullText       string        `json:"full_text"`
	Chain          []LongGenStep `json:"chain"`
	ChainLength    int           `json:"chain_length"`
	TotalTokens    int           `json:"total_tokens"`
	TotalNew       int           `json:"total_new"`
	HasReturn      bool          `json:"has_return"`
	HasLoop        bool          `json:"has_loop"`
	HasConditional bool          `json:"has_conditional"`
	LooksValid     bool          `json:"looks_valid"`
	ReachedReturn  bool          `json:"reached_return"`
	SourceCount    int           `json:"source_count"`
}

// LongGenResult holds the full long generation experiment results.
type LongGenResult struct {
	TotalSpans    int            `json:"total_spans"`
	CorpusSize    int            `json:"corpus_size"`
	MaxChains     int            `json:"max_chains"`
	SpanLengths   []int          `json:"span_lengths"`
	Entries       []LongGenEntry `json:"entries"`
	ValidCount    int            `json:"valid_count"`
	ReturnCount   int            `json:"return_count"`
	LoopCount     int            `json:"loop_count"`
	MeanTokens    float64        `json:"mean_tokens"`
	MeanNewTokens float64        `json:"mean_new_tokens"`
}

// CompGenStep holds one step in a compositional generation chain.
type CompGenStep struct {
	Step      int     `json:"step"`
	SpanText  string  `json:"span_text"`
	NewText   string  `json:"new_text"`
	NewTokens int     `json:"new_tokens"`
	Overlap   int     `json:"overlap"`
	SimScore  float64 `json:"sim_score"`
	SourceIdx int     `json:"source_idx"`
	SourceFn  string  `json:"source_fn"`
}

// CompGenEntry holds one prompt's compositional generation result.
type CompGenEntry struct {
	Desc            string        `json:"desc"`
	Prefix          string        `json:"prefix"`
	Expected        string        `json:"expected"`
	FullText        string        `json:"full_text"`
	Chain           []CompGenStep `json:"chain"`
	ChainLength     int           `json:"chain_length"`
	TotalTokens     int           `json:"total_tokens"`
	TotalNew        int           `json:"total_new"`
	HasReturn       bool          `json:"has_return"`
	HasLoop         bool          `json:"has_loop"`
	HasConditional bool          `json:"has_conditional"`
	ReachedReturn   bool          `json:"reached_return"`
	SourceCount     int           `json:"source_count"`
	ExpectedOverlap float64       `json:"expected_overlap"`
}

// CompGenResult holds the full compositional generation experiment results.
type CompGenResult struct {
	TotalSpans          int            `json:"total_spans"`
	Entries             []CompGenEntry `json:"entries"`
	ReturnCount         int            `json:"return_count"`
	LoopCount           int            `json:"loop_count"`
	MeanTokens          float64        `json:"mean_tokens"`
	MeanExpectedOverlap float64        `json:"mean_expected_overlap"`
}

// StructSensEntry holds one probe's structural sensitivity results.
type StructSensEntry struct {
	Name   string `json:"name"`
	Prefix string `json:"prefix"`

	SimPrefixFull  float64 `json:"sim_prefix_full"`
	SimCommentFull float64 `json:"sim_comment_full"`
	SimNoiseFull   float64 `json:"sim_noise_full"`
	SimCorrectFull float64 `json:"sim_correct_full"`

	DistComment float64 `json:"dist_comment"`
	DistNoise   float64 `json:"dist_noise"`
	DistCorrect float64 `json:"dist_correct"`

	DirComment float64 `json:"dir_comment"`
	DirNoise   float64 `json:"dir_noise"`
	DirCorrect float64 `json:"dir_correct"`

	CorrectBestSim bool `json:"correct_best_sim"`
	CorrectBestDir bool `json:"correct_best_dir"`
	CommentLeast   bool `json:"comment_least"`
}

// StructSensResult holds the full structural sensitivity experiment results.
type StructSensResult struct {
	Entries         []StructSensEntry `json:"entries"`
	BestSimCount    int               `json:"best_sim_count"`
	BestDirCount    int               `json:"best_dir_count"`
	LeastMoveCount  int               `json:"least_move_count"`
	StructSensCount int               `json:"struct_sens_count"`
}

// EigenmodeEntry holds per-role centroid statistics in PC space.
type EigenmodeEntry struct {
	Role    string  `json:"role"`
	Count   int     `json:"count"`
	MeanPC1 float64 `json:"mean_pc1"`
	MeanPC2 float64 `json:"mean_pc2"`
	MeanPC3 float64 `json:"mean_pc3"`
	StdPC1  float64 `json:"std_pc1"`
	StdPC2  float64 `json:"std_pc2"`
	StdPC3  float64 `json:"std_pc3"`
}

// EigenmodeSeparation holds pairwise centroid separation data.
type EigenmodeSeparation struct {
	RoleA     string  `json:"role_a"`
	RoleB     string  `json:"role_b"`
	Distance  float64 `json:"distance"`
	AvgSpread float64 `json:"avg_spread"`
	Ratio     float64 `json:"ratio"`
}

// EigenmodePoint is a single span projected into PC space.
type EigenmodePoint struct {
	PC1  float64 `json:"pc1"`
	PC2  float64 `json:"pc2"`
	PC3  float64 `json:"pc3"`
	Role string  `json:"role"`
}

// EigenmodeResult holds the full eigenmode probe results.
type EigenmodeResult struct {
	TotalSpans   int                   `json:"total_spans"`
	Roles        []EigenmodeEntry      `json:"roles"`
	Separations  []EigenmodeSeparation `json:"separations"`
	Points       []EigenmodePoint      `json:"points"`
	WellSepCount int                   `json:"well_sep_count"`
	TotalPairs   int                   `json:"total_pairs"`
}

// CantileverStep records one step of the cantilever-gated chainer.
type CantileverStep struct {
	StepNum    int     `json:"step"`
	SimScore   float64 `json:"sim_score"`
	SpanText   string  `json:"span_text"`
	NewTokens  int     `json:"new_tokens"`
	Overlap    int     `json:"overlap"`
	SourceFunc string  `json:"source_func"`
	SpanLen    int     `json:"span_len"`
	CantExtent int     `json:"cant_extent"`
	EigenPhase float64 `json:"eigen_phase"`
	Progress   float64 `json:"progress"`
	InBridge   bool    `json:"in_bridge"`
}

// CantileverEntry records one prompt's cantilever generation.
type CantileverEntry struct {
	Prefix      string           `json:"prefix"`
	Desc        string           `json:"desc"`
	FullText    string           `json:"full_text"`
	ChainLength int              `json:"chain_length"`
	TotalTokens int              `json:"total_tokens"`
	HasReturn   bool             `json:"has_return"`
	HasLoop     bool             `json:"has_loop"`
	BridgeCount int              `json:"bridge_count"`
	Gated       bool             `json:"gated"`
	Chain       []CantileverStep `json:"chain"`
}

// CantileverStats holds aggregate metrics for one arm of the cantilever experiment.
type CantileverStats struct {
	MeanTokens  float64 `json:"mean_tokens"`
	ReturnCount int     `json:"return_count"`
	LoopCount   int     `json:"loop_count"`
	BridgeCount int     `json:"bridge_count"`
}

// CantileverResult holds all cantilever-gated experiment results.
type CantileverResult struct {
	ControlEntries []CantileverEntry `json:"control"`
	GatedEntries   []CantileverEntry `json:"gated"`
	ControlStats   CantileverStats   `json:"control_stats"`
	GatedStats     CantileverStats   `json:"gated_stats"`
}

// ChordGenStep captures one step of chord-based generation.
type ChordGenStep struct {
	StepNum   int
	BestScore float64
	BestScale int
	BestPos   int
	HoleBits  int
	EmittedN  int
	Emitted   string
}

// ChordGenEntry holds the result for one chord generation prompt.
type ChordGenEntry struct {
	Prompt    string
	Generated string
	Steps     []ChordGenStep
	Tokens    int
	HasReturn bool
	HasColon  bool
}

// ChordGenResult is the output of chord generation.
type ChordGenResult struct {
	Entries    []ChordGenEntry
	StoreSize  int
	CorpusSize int
}

// PhaseBridgingStep records one step of the phase-bridging chainer.
type PhaseBridgingStep struct {
	StepNum       int     `json:"step"`
	SimScore      float64 `json:"sim_score"`
	SpanText      string  `json:"span_text"`
	NewTokens     int     `json:"new_tokens"`
	Overlap       int     `json:"overlap"`
	SourceFunc    string  `json:"source_func"`
	EigenPhase    float64 `json:"eigen_phase"`
	Concentration float64 `json:"concentration"`
	InBridge      bool    `json:"in_bridge"`
}

// PhaseBridgingEntry records one prompt's phase bridging generation.
type PhaseBridgingEntry struct {
	Prefix      string              `json:"prefix"`
	Desc        string              `json:"desc"`
	FullText    string              `json:"full_text"`
	ChainLength int                 `json:"chain_length"`
	TotalTokens int                 `json:"total_tokens"`
	HasReturn   bool                `json:"has_return"`
	HasLoop     bool                `json:"has_loop"`
	BridgeCount int                 `json:"bridge_count"`
	Chain       []PhaseBridgingStep `json:"chain"`
}

// PhaseBridgingResult holds all phase bridging results.
type PhaseBridgingResult struct {
	Entries     []PhaseBridgingEntry `json:"entries"`
	MeanTokens  float64             `json:"mean_tokens"`
	ReturnCount int                 `json:"return_count"`
	LoopCount   int                 `json:"loop_count"`
	BridgeTotal int                 `json:"bridge_total"`
}

// RelCantStep records one step of the relative-cantilever chainer.
type RelCantStep struct {
	StepNum    int     `json:"step"`
	SimScore   float64 `json:"sim_score"`
	SpanText   string  `json:"span_text"`
	NewTokens  int     `json:"new_tokens"`
	Overlap    int     `json:"overlap"`
	SourceFunc string  `json:"source_func"`
	SpanLen    int     `json:"span_len"`
	MaxSafe    int     `json:"max_safe"`
	EigenPhase float64 `json:"eigen_phase"`
	Progress   float64 `json:"progress"`
	InBridge   bool    `json:"in_bridge"`
}

// RelCantEntry records one prompt's relative cantilever generation.
type RelCantEntry struct {
	Prefix      string        `json:"prefix"`
	Desc        string        `json:"desc"`
	FullText    string        `json:"full_text"`
	ChainLength int           `json:"chain_length"`
	TotalTokens int           `json:"total_tokens"`
	HasReturn   bool          `json:"has_return"`
	HasLoop     bool          `json:"has_loop"`
	BridgeCount int           `json:"bridge_count"`
	Gated       bool          `json:"gated"`
	Chain       []RelCantStep `json:"chain"`
}

// RelCantResult holds all relative cantilever results.
type RelCantResult struct {
	ControlEntries []RelCantEntry  `json:"control"`
	GatedEntries   []RelCantEntry  `json:"gated"`
	ControlStats   CantileverStats `json:"control_stats"`
	GatedStats     CantileverStats `json:"gated_stats"`
}
