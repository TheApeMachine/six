package telemetry

/*
Event is the JSON shape consumed by the visualizer WebSocket clients.
All fields use lowercase JSON names to match the browser.
*/
type Event struct {
	Component string    `json:"component"`
	Action    string    `json:"action"`
	Data      EventData `json:"data"`
}

/*
EventData carries optional telemetry payload. Only non-zero / non-empty
fields are serialized (omitempty).
*/
type EventData struct {
	Stage   string `json:"stage,omitempty"`
	Message string `json:"message,omitempty"`

	ChunkText string `json:"chunkText,omitempty"`
	Frame     int    `json:"frameIndex,omitempty"`

	Instruction string `json:"instruction,omitempty"`
	DataPop     int    `json:"dataPop,omitempty"`
	OperandPop  int    `json:"operandPop,omitempty"`
	AccumPop    int    `json:"accumPop,omitempty"`

	EdgeCount  int     `json:"edgeCount,omitempty"`
	PathCount  int     `json:"pathCount,omitempty"`
	Paths      int     `json:"paths,omitempty"`
	EntryCount int     `json:"entryCount,omitempty"`
	Bin        int     `json:"bin,omitempty"`
	Level      int     `json:"level,omitempty"`
	ChildCount int     `json:"childCount,omitempty"`
	Density    float64 `json:"density,omitempty"`

	ResultText string `json:"resultText,omitempty"`
	Msg        string `json:"msg,omitempty"`

	PreResidue     int `json:"preResidue,omitempty"`
	PostResidue    int `json:"postResidue,omitempty"`
	Step           int `json:"step,omitempty"`
	CandidateCount int `json:"candidateCount,omitempty"`
	MaxSteps       int `json:"maxSteps,omitempty"`

	JobID       string `json:"jobId,omitempty"`
	TaskType    string `json:"taskType,omitempty"`
	DurationMs  int    `json:"durationMs,omitempty"`
	QueueSize   int    `json:"queueSize,omitempty"`
	WorkerCount int    `json:"workerCount,omitempty"`
	IdleWorkers int    `json:"idleWorkers,omitempty"`

	BestIndex int    `json:"bestIndex,omitempty"`
	Avail     int    `json:"avail,omitempty"`
	NodeAddr  string `json:"nodeAddr,omitempty"`
	NodeCount int    `json:"nodeCount,omitempty"`

	Chunks int `json:"chunks,omitempty"`

	ActiveBits []int `json:"activeBits,omitempty"`

	NodeID     uint64 `json:"nodeId,omitempty"`
	NodeTokens string `json:"nodeTokens,omitempty"`
	NodeType   string `json:"nodeType,omitempty"`
	FromID     uint64 `json:"fromId,omitempty"`
	ToID       uint64 `json:"toId,omitempty"`
}
