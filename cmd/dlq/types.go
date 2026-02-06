package main

type jobView struct {
	ID            int64  `json:"id"`
	URL           string `json:"url"`
	Site          string `json:"site"`
	OutDir        string `json:"out_dir"`
	Name          string `json:"name"`
	Status        string `json:"status"`
	Filename      string `json:"filename"`
	SizeBytes     int64  `json:"size_bytes"`
	BytesDone     int64  `json:"bytes_done"`
	DownloadSpeed int64  `json:"download_speed"`
	EtaSeconds    int64  `json:"eta_seconds"`
	Error         string `json:"error"`
	ErrorCode     string `json:"error_code"`
	CreatedAt     string `json:"created_at"`
	UpdatedAt     string `json:"updated_at"`
}
