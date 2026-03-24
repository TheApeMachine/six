package experiment

type BarSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

type BarChartData struct {
	XAxis  []string    `json:"x_axis"`
	Series []BarSeries `json:"series"`
}

type LineSeries struct {
	Name string    `json:"name"`
	Data []float64 `json:"data"`
}

type LineChartData struct {
	XAxis  []string     `json:"x_axis"`
	Series []LineSeries `json:"series"`
	YMin   float64      `json:"y_min"`
	YMax   float64      `json:"y_max"`
}

type ComboSeries struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Symbol   string    `json:"symbol,omitempty"`
	BarWidth string    `json:"bar_width,omitempty"`
	Data     []float64 `json:"data"`
}

type ComboChartData struct {
	XAxis  []string      `json:"x_axis"`
	Series []ComboSeries `json:"series"`
	XName  string        `json:"x_name"`
	YName  string        `json:"y_name"`
	YMin   float64       `json:"y_min"`
	YMax   float64       `json:"y_max"`
}

type HeatMapData struct {
	XAxis []string `json:"x_axis"`
	YAxis []string `json:"y_axis"`
	Data  [][]any  `json:"data"`
	Min   float64  `json:"min"`
	Max   float64  `json:"max"`
}

type ConfusionMatrixData struct {
	Labels    []string `json:"labels"`
	Matrix    [][]int  `json:"matrix"`
	MeanScore float64  `json:"mean_score"`
}

type PanelSeries struct {
	Name     string    `json:"name"`
	Kind     string    `json:"kind"`
	Symbol   string    `json:"symbol,omitempty"`
	BarWidth string    `json:"bar_width,omitempty"`
	Area     bool      `json:"area,omitempty"`
	Data     []float64 `json:"data"`
	Color    string    `json:"color,omitempty"`
}

type Panel struct {
	Kind string `json:"kind"`

	Title string `json:"title"`

	GridLeft   string `json:"grid_left,omitempty"`
	GridRight  string `json:"grid_right,omitempty"`
	GridTop    string `json:"grid_top,omitempty"`
	GridBottom string `json:"grid_bottom,omitempty"`

	XLabels   []string `json:"x_labels,omitempty"`
	XAxisName string   `json:"x_axis_name,omitempty"`
	XInterval int      `json:"x_interval,omitempty"`
	XShow     bool     `json:"x_show,omitempty"`

	YLabels     []string `json:"y_labels,omitempty"`
	YAxisName   string   `json:"y_axis_name,omitempty"`
	YInterval   int      `json:"y_interval,omitempty"`
	HeatData    [][]any  `json:"heat_data,omitempty"`
	HeatMin     float64  `json:"heat_min,omitempty"`
	HeatMax     float64  `json:"heat_max,omitempty"`
	ColorScheme string   `json:"color_scheme,omitempty"`
	ShowVM      bool     `json:"show_vm,omitempty"`
	VMRight     string   `json:"vm_right,omitempty"`

	Series []PanelSeries `json:"series,omitempty"`
	YMin   *float64      `json:"y_min,omitempty"`
	YMax   *float64      `json:"y_max,omitempty"`
}

type MultiPanelData struct {
	Panels []Panel `json:"panels"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
}

type ProseData struct {
	Template string         `json:"template"`
	Data     map[string]any `json:"data"`
}

type ImageStripRow struct {
	Original      string `json:"original"`
	Masked        string `json:"masked"`
	Reconstructed string `json:"reconstructed"`
	Label         string `json:"label"`
}

type ImageStripData struct {
	Rows []ImageStripRow `json:"rows"`
}

func Float64Ptr(v float64) *float64 {
	return &v
}
