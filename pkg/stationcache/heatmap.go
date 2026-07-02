package stationcache

// HeatPoint is one aggregated heatmap bucket: a coordinate and the total
// number of directly-received packets attributed to it in the query window.
type HeatPoint struct {
	Lat   float64
	Lon   float64
	Count int
}

// HeatmapResult is the aggregate returned by a heatmap query. Points are the
// located buckets, MaxCount is the largest single-bucket count (for client-side
// weight normalization), and Unlocatable counts packets whose attributed
// transmitter had no known position in the window.
type HeatmapResult struct {
	Points      []HeatPoint
	MaxCount    int
	Unlocatable int
}
