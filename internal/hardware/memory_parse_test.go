package hardware

import "testing"

func TestParseVMStatAvailable(t *testing.T) {
	text := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               1000.
Pages active:                            20000.
Pages inactive:                           2000.
Pages speculative:                         500.
Pages wired down:                         8000.
`
	got, ok := parseVMStatAvailable(text)
	if !ok {
		t.Fatal("expected parse")
	}
	want := uint64(3500 * 16384)
	if got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestParseSwapUsage(t *testing.T) {
	total, used, ok := parseSwapUsage("total = 2048.00M  used = 512.00M  free = 1536.00M")
	if !ok {
		t.Fatal("expected parse")
	}
	if total != 2048*1024*1024 || used != 512*1024*1024 {
		t.Fatalf("total %d used %d", total, used)
	}
}

func TestCapByCgroup(t *testing.T) {
	const gib = 1 << 30
	for _, c := range []struct {
		name                 string
		total, avail         uint64
		max, current         string
		wantTotal, wantAvail uint64
	}{
		{"no limit", 64 * gib, 40 * gib, "max\n", "", 64 * gib, 40 * gib},
		{"unreadable", 64 * gib, 40 * gib, "", "", 64 * gib, 40 * gib},
		{"limit above the machine", 64 * gib, 40 * gib, "137438953472\n", "", 64 * gib, 40 * gib},
		{"container limit", 64 * gib, 40 * gib, "17179869184\n", "4294967296\n", 16 * gib, 12 * gib},
		{"machine busier than the limit", 64 * gib, 8 * gib, "17179869184\n", "1073741824\n", 16 * gib, 8 * gib},
		{"over the limit", 64 * gib, 40 * gib, "17179869184\n", "18253611008\n", 16 * gib, 0},
		{"limit without usage", 64 * gib, 40 * gib, "17179869184\n", "", 16 * gib, 16 * gib},
	} {
		total, avail := capByCgroup(c.total, c.avail, c.max, c.current)
		if total != c.wantTotal || avail != c.wantAvail {
			t.Errorf("%s: got %d, %d; want %d, %d", c.name, total, avail, c.wantTotal, c.wantAvail)
		}
	}
	if g, ok := cgroupGroup("0::/system.slice/toskar.service\n"); !ok || g != "/system.slice/toskar.service" {
		t.Errorf("cgroup path: %q %v", g, ok)
	}
	if _, ok := cgroupGroup("12:memory:/docker/abc\n"); ok {
		t.Error("cgroup v1 read as v2")
	}
}
