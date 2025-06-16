package metrics

import (
	"testing"
	"time"
)

func TestMemoryMetrics_CapacityPercent(t *testing.T) {
	tests := []struct {
		name        string
		memory      MemoryMetrics
		expected    float64
		expectError bool
	}{
		{
			name: "normal case - 50% usage",
			memory: MemoryMetrics{
				MaxCapacity:  100.0,
				UsedCapacity: 50.0,
			},
			expected:    50.0,
			expectError: false,
		},
		{
			name: "normal case - 75% usage",
			memory: MemoryMetrics{
				MaxCapacity:  8192.0,
				UsedCapacity: 6144.0,
			},
			expected:    75.0,
			expectError: false,
		},
		{
			name: "normal case - 100% usage",
			memory: MemoryMetrics{
				MaxCapacity:  1000.0,
				UsedCapacity: 1000.0,
			},
			expected:    100.0,
			expectError: false,
		},
		{
			name: "normal case - 0% usage",
			memory: MemoryMetrics{
				MaxCapacity:  1000.0,
				UsedCapacity: 0.0,
			},
			expected:    0.0,
			expectError: false,
		},
		{
			name: "error case - max capacity is 0",
			memory: MemoryMetrics{
				MaxCapacity:  0.0,
				UsedCapacity: 50.0,
			},
			expected:    0.0,
			expectError: true,
		},
		{
			name: "edge case - very small numbers",
			memory: MemoryMetrics{
				MaxCapacity:  0.001,
				UsedCapacity: 0.0005,
			},
			expected:    50.0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.memory.CapacityPercent()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

func TestDiskMetrics_UsagePercent(t *testing.T) {
	tests := []struct {
		name        string
		disk        DiskMetrics
		expected    float64
		expectError bool
	}{
		{
			name: "normal case - 60% usage",
			disk: DiskMetrics{
				TotalCapacity:     1000.0,
				UsedCapacity:      600.0,
				AvailableCapacity: 400.0,
			},
			expected:    60.0,
			expectError: false,
		},
		{
			name: "normal case - 25% usage",
			disk: DiskMetrics{
				TotalCapacity:     512000.0,
				UsedCapacity:      128000.0,
				AvailableCapacity: 384000.0,
			},
			expected:    25.0,
			expectError: false,
		},
		{
			name: "normal case - 100% usage",
			disk: DiskMetrics{
				TotalCapacity:     2000.0,
				UsedCapacity:      2000.0,
				AvailableCapacity: 0.0,
			},
			expected:    100.0,
			expectError: false,
		},
		{
			name: "normal case - 0% usage",
			disk: DiskMetrics{
				TotalCapacity:     500.0,
				UsedCapacity:      0.0,
				AvailableCapacity: 500.0,
			},
			expected:    0.0,
			expectError: false,
		},
		{
			name: "error case - total capacity is 0",
			disk: DiskMetrics{
				TotalCapacity:     0.0,
				UsedCapacity:      100.0,
				AvailableCapacity: 0.0,
			},
			expected:    0.0,
			expectError: true,
		},
		{
			name: "edge case - very small numbers",
			disk: DiskMetrics{
				TotalCapacity:     0.002,
				UsedCapacity:      0.001,
				AvailableCapacity: 0.001,
			},
			expected:    50.0,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.disk.UsagePercent()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected an error, but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, result)
			}
		})
	}
}

// TestSysMetrics tests the SysMetrics struct creation and field access
func TestSysMetrics(t *testing.T) {
	now := time.Now()
	sysMetrics := SysMetrics{
		CPU: CPUMetrics{
			MaxFrequency:       3500.0,
			CurrentFrequency:   2800.0,
			MaxTemperature:     85.0,
			CurrentTemperature: 65.0,
		},
		GPU: GPUMetrics{
			MaxFrequency:       1800.0,
			CurrentFrequency:   1200.0,
			CurrentTemperature: 70.0,
			MaxTemperature:     90.0,
		},
		Memory: MemoryMetrics{
			MaxCapacity:  16384.0,
			UsedCapacity: 8192.0,
		},
		Screen: ScreenMetrics{
			Width:       1920,
			Height:      1080,
			RefreshRate: 60.0,
		},
		Disk: DiskMetrics{
			TotalCapacity:     1000000.0,
			UsedCapacity:      500000.0,
			AvailableCapacity: 500000.0,
		},
		Timestamp: now,
	}

	// Test that all fields are accessible and have expected values
	if sysMetrics.CPU.MaxFrequency != 3500.0 {
		t.Errorf("expected CPU max frequency 3500.0, got %f", sysMetrics.CPU.MaxFrequency)
	}

	if sysMetrics.GPU.CurrentFrequency != 1200.0 {
		t.Errorf("expected GPU current frequency 1200.0, got %f", sysMetrics.GPU.CurrentFrequency)
	}

	if sysMetrics.Screen.Width != 1920 {
		t.Errorf("expected screen width 1920, got %d", sysMetrics.Screen.Width)
	}

	if !sysMetrics.Timestamp.Equal(now) {
		t.Errorf("expected timestamp %v, got %v", now, sysMetrics.Timestamp)
	}

	// Test memory percentage calculation
	memPercent, err := sysMetrics.Memory.CapacityPercent()
	if err != nil {
		t.Errorf("unexpected error calculating memory percentage: %v", err)
	}
	if memPercent != 50.0 {
		t.Errorf("expected memory percentage 50.0, got %f", memPercent)
	}

	// Test disk percentage calculation
	diskPercent, err := sysMetrics.Disk.UsagePercent()
	if err != nil {
		t.Errorf("unexpected error calculating disk percentage: %v", err)
	}
	if diskPercent != 50.0 {
		t.Errorf("expected disk percentage 50.0, got %f", diskPercent)
	}
}

// TestStructFieldTypes tests that all struct fields have the correct types
func TestStructFieldTypes(t *testing.T) {
	t.Run("CPUMetrics fields", func(t *testing.T) {
		cpu := CPUMetrics{}
		_ = cpu.MaxFrequency       // float64
		_ = cpu.CurrentFrequency   // float64
		_ = cpu.MaxTemperature     // float64
		_ = cpu.CurrentTemperature // float64
	})

	t.Run("GPUMetrics fields", func(t *testing.T) {
		gpu := GPUMetrics{}
		_ = gpu.MaxFrequency       // float64
		_ = gpu.CurrentFrequency   // float64
		_ = gpu.CurrentTemperature // float64
		_ = gpu.MaxTemperature     // float64
	})

	t.Run("ScreenMetrics fields", func(t *testing.T) {
		screen := ScreenMetrics{}
		_ = screen.Width       // int
		_ = screen.Height      // int
		_ = screen.RefreshRate // float64
	})
}
