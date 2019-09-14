package nilarg_test

import (
	"fmt"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"golang.org/x/tools/go/analysis/passes/nilarg"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	result := analysistest.Run(t, testdata, nilarg.Analyzer, "a")[0].Result

	panicArgs := result.(nilarg.PanicArgs)
	got := fmt.Sprint(panicArgs)
	want := `map[a.f:map[1:{} 3:{}] a.f2:map[0:{} 1:{}]]`
	if got != want {
		t.Errorf("PanicArgs = %s, want %s", got, want)
	}
}
