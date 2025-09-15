package types

import (
	"errors"
	"time"
)

type CPUMetrics struct {
	MaxFrequency       float64 `json:"max_frequency"`
	CurrentFrequency   float64 `json:"current_frequency"`
	MaxTemperature     float64 `json:"max_temperature"`
	CurrentTemperature float64 `json:"current_temperature"`
}

type GPUMetrics struct {
	MaxFrequency       float64 `json:"max_frequency"`
	CurrentFrequency   float64 `json:"current_frequency"`
	CurrentTemperature float64 `json:"current_temperature"`
	MaxTemperature     float64 `json:"max_temperature"`
}

type MemoryMetrics struct {
	MaxCapacity  float64 `json:"max_capacity"`
	UsedCapacity float64 `json:"used_capacity"`
}

func (p MemoryMetrics) CapacityPercent() (float64, error) {
	if p.MaxCapacity == 0 {
		return 0, errors.New("max capacity is 0")
	}
	return p.UsedCapacity / p.MaxCapacity * 100, nil
}

type ScreenMetrics struct {
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	RefreshRate float64 `json:"refresh_rate"`
}

type DiskMetrics struct {
	TotalCapacity     float64 `json:"total_capacity"`
	UsedCapacity      float64 `json:"used_capacity"`
	AvailableCapacity float64 `json:"available_capacity"`
}

func (p DiskMetrics) UsagePercent() (float64, error) {
	if p.TotalCapacity == 0 {
		return 0, errors.New("total capacity is 0")
	}
	return p.UsedCapacity / p.TotalCapacity * 100, nil
}

type SysMetrics struct {
	CPU       CPUMetrics    `json:"cpu"`
	GPU       GPUMetrics    `json:"gpu"`
	Memory    MemoryMetrics `json:"memory"`
	Screen    ScreenMetrics `json:"screen"`
	Uptime    float64       `json:"uptime"`
	Disk      DiskMetrics   `json:"disk"`
	Timestamp time.Time     `json:"timestamp"` // Unix timestamp
}
