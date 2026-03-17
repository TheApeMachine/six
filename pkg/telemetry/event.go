package telemetry

/*
Event is a telemetry event sent to all connected visualization clients.
*/
type Event struct {
	Component string    `json:"component"`
	Action    string    `json:"action"`
	Data      EventData `json:"data"`
}

/*
EventData carries the payload for a visualization event.
*/
type EventData struct {
	ValueID int    `json:"valueId,omitempty"`
	Bin     int    `json:"bin,omitempty"`
	State   string `json:"state,omitempty"`

	ActiveBits []int   `json:"activeBits,omitempty"`
	Density    float64 `json:"density,omitempty"`
	ChunkText  string  `json:"chunkText,omitempty"`

	Residue    int   `json:"residue,omitempty"`
	MatchBits  []int `json:"matchBits,omitempty"`
	CancelBits []int `json:"cancelBits,omitempty"`

	Left  int `json:"left,omitempty"`
	Right int `json:"right,omitempty"`
	Pos   int `json:"pos,omitempty"`

	Paths  int `json:"paths,omitempty"`
	Chunks int `json:"chunks,omitempty"`
	Edges  int `json:"edges,omitempty"`

	Level      int     `json:"level,omitempty"`
	Theta      float64 `json:"theta,omitempty"`
	ParentBin  int     `json:"parentBin,omitempty"`
	ChildCount int     `json:"childCount,omitempty"`

	Stage           string `json:"stage,omitempty"`
	Message         string `json:"message,omitempty"`
	EdgeCount       int    `json:"edgeCount,omitempty"`
	PathCount       int    `json:"pathCount,omitempty"`
	ResultText      string `json:"resultText,omitempty"`
	WavefrontEnergy int    `json:"wavefrontEnergy,omitempty"`
	EntryCount      int    `json:"entryCount,omitempty"`
}
