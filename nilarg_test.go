package nilarg_test

import (
	"reflect"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
	"github.com/Matts966/nilarg"
)

func Test(t *testing.T) {
	testdata := analysistest.TestData()
	result := analysistest.Run(t, testdata, nilarg.Analyzer, "a")[0].Result
	got := result.(nilarg.PanicArgs)
	want := nilarg.PanicArgs{
		"(*bytes.Buffer).Bytes":        map[int]struct{}{0: struct{}{}},
		"(*net/http.Response).Cookies": map[int]struct{}{0: struct{}{}},
		"a.f":  map[int]struct{}{1: struct{}{}, 3: struct{}{}},
		"a.f2": map[int]struct{}{0: struct{}{}, 1: struct{}{}, 2: struct{}{}, 3: struct{}{}},
		"a.f3": map[int]struct{}{0: struct{}{}},
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("PanicArgs = %#v\nbut want = %#v", got, want)
	}
}
