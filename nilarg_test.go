package nilarg_test

import (
	"fmt"
	"log"
	"testing"

	"github.com/Matts966/nilarg"

	"golang.org/x/tools/go/analysis/analysistest"
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

func TestBytes(t *testing.T) {
	log.Println(analysistest.Run(t, "", nilarg.Analyzer, "bytes"))
	panic(nil)
}
