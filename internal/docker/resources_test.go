package docker

import (
	"strings"
	"testing"
)

func pids(n int64) *int64 {
	return &n
}

func TestRestoreTargetMemory(t *testing.T) {
	const mb = 1 << 20

	tests := []struct {
		name         string
		original     Resources
		wantMemory   int64
		wantSwap     int64
		wantsWarning bool
	}{
		{
			name:         "no original limit cannot be expressed",
			original:     Resources{},
			wantsWarning: true,
		},
		{
			// The squeeze pinned swap to the squeezed memory, so a restore that
			// only raised Memory would be refused by the kernel.
			name:       "unset swap restores to twice the memory",
			original:   Resources{Memory: 512 * mb},
			wantMemory: 512 * mb,
			wantSwap:   1024 * mb,
		},
		{
			name:       "explicit swap comes back as it was",
			original:   Resources{Memory: 512 * mb, MemorySwap: 600 * mb},
			wantMemory: 512 * mb,
			wantSwap:   600 * mb,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore, warnings := restoreTarget(tt.original)

			if restore.Memory != tt.wantMemory {
				t.Errorf("Memory = %d, want %d", restore.Memory, tt.wantMemory)
			}

			if restore.MemorySwap != tt.wantSwap {
				t.Errorf("MemorySwap = %d, want %d", restore.MemorySwap, tt.wantSwap)
			}

			warned := false
			for _, w := range warnings {
				if strings.Contains(w, "memory") {
					warned = true
				}
			}

			if warned != tt.wantsWarning {
				t.Errorf("memory warning = %v, want %v (warnings: %v)", warned, tt.wantsWarning, warnings)
			}
		})
	}
}

func TestRestoreTargetCPU(t *testing.T) {
	tests := []struct {
		name       string
		original   Resources
		wantNano   int64
		wantQuota  int64
		wantPeriod int64
	}{
		{
			// Zero means "leave alone", so unlimited has to be said with -1.
			name:      "no original limit restores an unlimited quota",
			original:  Resources{},
			wantQuota: -1,
		},
		{
			name:     "nanocpus wins, because the daemon prefers it over quota",
			original: Resources{NanoCpus: 2e9, CpuQuota: 50000, CpuPeriod: 100000},
			wantNano: 2e9,
		},
		{
			name:       "quota and period come back together",
			original:   Resources{CpuQuota: 50000, CpuPeriod: 200000},
			wantQuota:  50000,
			wantPeriod: 200000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore, _ := restoreTarget(tt.original)

			if restore.NanoCpus != tt.wantNano {
				t.Errorf("NanoCpus = %d, want %d", restore.NanoCpus, tt.wantNano)
			}

			if restore.CpuQuota != tt.wantQuota {
				t.Errorf("CpuQuota = %d, want %d", restore.CpuQuota, tt.wantQuota)
			}

			if restore.CpuPeriod != tt.wantPeriod {
				t.Errorf("CpuPeriod = %d, want %d", restore.CpuPeriod, tt.wantPeriod)
			}
		})
	}
}

func TestRestoreTargetPids(t *testing.T) {
	tests := []struct {
		name     string
		original Resources
		want     int64
	}{
		{name: "recorded limit", original: Resources{PidsLimit: pids(4096)}, want: 4096},
		{name: "absent limit", original: Resources{}, want: -1},
		{name: "zero limit means unlimited", original: Resources{PidsLimit: pids(0)}, want: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			restore, _ := restoreTarget(tt.original)

			if restore.PidsLimit == nil {
				t.Fatal("PidsLimit is nil, which the daemon reads as no change")
			}

			if *restore.PidsLimit != tt.want {
				t.Errorf("PidsLimit = %d, want %d", *restore.PidsLimit, tt.want)
			}
		})
	}
}

func TestRestoreTargetBlkio(t *testing.T) {
	restore, _ := restoreTarget(Resources{BlkioWeight: 700})
	if restore.BlkioWeight != 700 {
		t.Errorf("BlkioWeight = %d, want 700", restore.BlkioWeight)
	}

	restore, warnings := restoreTarget(Resources{})
	if restore.BlkioWeight != blkioDefault {
		t.Errorf("BlkioWeight = %d, want the cgroup default %d", restore.BlkioWeight, blkioDefault)
	}

	if len(warnings) == 0 {
		t.Error("want a warning that the original weight was unset")
	}
}

func TestFormatResources(t *testing.T) {
	out := FormatResources(Resources{})
	for _, want := range []string{"memory       unlimited", "cpus         unlimited", "pids         unlimited", "blkio        unset"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatResources() is missing %q:\n%s", want, out)
		}
	}

	out = FormatResources(Resources{Memory: 64 << 20, CpuQuota: 50000, CpuPeriod: 100000, PidsLimit: pids(12), BlkioWeight: 10})
	for _, want := range []string{"64MB", "0.50", "pids         12", "blkio        10"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatResources() is missing %q:\n%s", want, out)
		}
	}

	// NanoCpus is reported as itself, since that is the knob a restore uses.
	if out := FormatResources(Resources{NanoCpus: 1500000000}); !strings.Contains(out, "1.50 (NanoCpus)") {
		t.Errorf("FormatResources() is missing the NanoCpus reading:\n%s", out)
	}
}

func TestRestoreWithoutASnapshotFails(t *testing.T) {
	// No daemon call should happen: there is nothing on record to put back.
	if _, _, err := Restore(nil, "never-squeezed"); err == nil {
		t.Fatal("want an error when nothing was recorded")
	}
}
