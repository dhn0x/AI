//go:build windows

package main

import "testing"

func TestTypedOK(t *testing.T) {
	long := "Get-Process | Sort-Object CPU -Descending | Select-Object -First 3 Name"
	cases := []struct {
		row, cmd string
		want     bool
	}{
		{"[csadmin@fortisoar ~]$ hostname", "hostname", true},
		{"PS> hostname   hostname; uptime", "hostname", true},        // grey suggestion after it
		{"[csadmin@fortisoar ~]$ ho", "hostname", false},               // rest went to the other display
		{"[csadmin@fortisoar ~]$ hostnam", "hostname", false},
		{"[csadmin@test   Get-Process   Sort-0bj ect CPU -Descending   Select-Obj ect   3 Name", long, true}, // OCR lost | and -First
		{"[csadmin@test   Get-Process   Sort-Object CPU -Desc", long, false},                               // end missing
		{"$ df -h | grep -v tmpfs", "df -h | grep -v tmpfs", true},
		{"$ rm -rf /var/log/cyops/*", "rm -rf /var/log/cyops/old/*", false}, // risky: must be exact
		{"$ systemctl status cyops-api", "systemctl status cyops-api", true},
	}
	for _, c := range cases {
		if got := typedOK(c.row, c.cmd); got != c.want {
			t.Errorf("typedOK(%q, %q) = %v, want %v", c.row, c.cmd, got, c.want)
		}
	}
}
