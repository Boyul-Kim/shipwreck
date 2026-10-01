package docker

import (
	"strings"
	"testing"
)

func TestParseProcesses(t *testing.T) {
	stdout := strings.Join([]string{
		"31 sh -c while :; do :; done # " + burnMarker,
		"1 /usr/local/bin/postgres -D /var/lib/postgresql/data",
		"9 [kworker]",
		"",
		"not-a-pid whatever",
		"44 sh -c for d in /proc/... # " + scanMarker,
		"7 postgres: checkpointer",
	}, "\n")

	got := parseProcesses(stdout)

	if len(got) != 4 {
		t.Fatalf("parsed %d processes, want 4: %+v", len(got), got)
	}

	wantPIDs := []int{1, 7, 9, 31}
	for i, want := range wantPIDs {
		if got[i].PID != want {
			t.Errorf("process %d has PID %d, want %d (sorted by PID)", i, got[i].PID, want)
		}
	}

	if got[0].Cmd != "/usr/local/bin/postgres -D /var/lib/postgresql/data" {
		t.Errorf("PID 1 command = %q", got[0].Cmd)
	}

	// The command line carries arguments, which is the whole point of reading
	// cmdline rather than comm: two workers are told apart by their arguments.
	if !strings.Contains(got[1].Cmd, "checkpointer") {
		t.Errorf("PID 7 command = %q, want the full command line", got[1].Cmd)
	}

	if !got[3].Burner() {
		t.Errorf("PID 31 should be recognised as a shipwreck burner: %q", got[3].Cmd)
	}

	if got[0].Burner() {
		t.Errorf("PID 1 should not be recognised as a burner: %q", got[0].Cmd)
	}
}

// The listing shell would otherwise show up in its own output, and a kill
// against it would be pointless.
func TestParseProcessesHidesTheScan(t *testing.T) {
	for _, p := range parseProcesses("5 /bin/sh -c echo hi # " + scanMarker) {
		t.Errorf("the scan was not filtered out: %+v", p)
	}
}

func TestProcessRowsAnnotatesInitAndBurners(t *testing.T) {
	columns, rows := ProcessRows([]Process{
		{PID: 1, Cmd: "nginx: master process"},
		{PID: 12, Cmd: "sh -c while :; do :; done # " + burnMarker},
		{PID: 13, Cmd: "nginx: worker process"},
	})

	if !strings.Contains(columns, "PID") {
		t.Errorf("columns = %q", columns)
	}

	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}

	if !strings.Contains(rows[0], "init") {
		t.Errorf("PID 1 row should warn that it ends the container: %q", rows[0])
	}

	if !strings.Contains(rows[1], "burner") {
		t.Errorf("burner row should be marked: %q", rows[1])
	}

	if strings.Contains(rows[2], "<-") {
		t.Errorf("an ordinary process should not be annotated: %q", rows[2])
	}
}

func TestNetworkRowsShowTheAttachedAddress(t *testing.T) {
	networks := []Network{
		{Name: "app_default", Driver: "bridge", Scope: "local"},
		{Name: "app_db", Driver: "bridge", Scope: "local", Internal: true},
	}

	attached := map[string]EndpointSettings{
		"app_default": {IPAddress: "172.18.0.4"},
	}

	_, rows := NetworkRows(networks, attached)

	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	if !strings.Contains(rows[0], "172.18.0.4") {
		t.Errorf("row = %q, want the endpoint address", rows[0])
	}

	if !strings.Contains(rows[1], "internal") {
		t.Errorf("row = %q, want the internal marker", rows[1])
	}

	// A network with no endpoint has no address to show.
	if !strings.Contains(rows[1], "-") {
		t.Errorf("row = %q, want a placeholder address", rows[1])
	}
}
