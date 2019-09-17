package nilarg_test

import (
	"reflect"
	"testing"

	"github.com/Matts966/nilarg"
	"golang.org/x/tools/go/analysis/analysistest"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	result := analysistest.Run(t, testdata, nilarg.Analyzer, "a")[0].Result

	panicArgs := result.(nilarg.PanicArgs)
	got := panicArgs
	want := map[string]map[int]struct{}{
		"(*bytes.Buffer).Bytes": map[int]struct{}{0: {}},
		"a.f":                   map[int]struct{}{1: {}, 3: {}},
		"a.f2":                  map[int]struct{}{0: {}, 1: {}},
	}
	if reflect.DeepEqual(got, want) {
		t.Errorf("PanicArgs = %#v, want %#v", got, want)
	}
}
