package model

import "time"

type File struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256,omitempty"`
}

type Manifest struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Parameters  string    `json:"parameters,omitempty"`
	Quant       string    `json:"quantization,omitempty"`
	Source      string    `json:"source"`
	Format      string    `json:"format"`
	Task        []string  `json:"task,omitempty"`
	License     string    `json:"license,omitempty"`
	Files       []File    `json:"files"`
	Runtimes    []string  `json:"runtimes,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	LocalPath   string    `json:"localPath,omitempty"`
	Available   bool      `json:"available"`
	SizeBytes   int64     `json:"sizeBytes,omitempty"`
	LastScanned time.Time `json:"lastScanned,omitempty"`
	Managed     bool      `json:"managed"`
}

type Task struct {
	ID          string    `json:"id"`
	ModelID     string    `json:"modelId"`
	Status      string    `json:"status"`
	BytesTotal  int64     `json:"bytesTotal"`
	BytesDone   int64     `json:"bytesDone"`
	CurrentFile string    `json:"currentFile,omitempty"`
	Error       string    `json:"error,omitempty"`
	FailureKind string    `json:"failureKind,omitempty"`
	SpeedBPS    int64     `json:"speedBps,omitempty"`
	ETASeconds  int64     `json:"etaSeconds,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
