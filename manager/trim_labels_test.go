package manager

import "testing"

var tc = [][]string{
	{"riscv-builders", "eu", "fr", "visionfive2", "DONOTDELETEME"},
	{},
	{"riscv-builders"},
}

func TestTrimLabels(t *testing.T) {
	for _, x := range tc {
		t.Log(trimLabels(x))
	}
}
