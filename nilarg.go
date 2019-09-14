package nilarg

import (
	"go/token"
	"go/types"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

const Doc = `check if the function will panic on nil arguments
`

var Analyzer = &analysis.Analyzer{
	Name:       "nilarg",
	Doc:        Doc,
	Run:        run,
	Requires:   []*analysis.Analyzer{buildssa.Analyzer},
	ResultType: reflect.TypeOf(PanicArgs{}),
}

// PanicArgs has the information about arguments which causes panic on calling the function when it is nil.
type PanicArgs map[*ssa.Function]map[int]struct{}

func run(pass *analysis.Pass) (interface{}, error) {
	pa := PanicArgs{}
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		runFunc(pass, fn, pa)
	}
	return pa, nil
}

func runFunc(pass *analysis.Pass, fn *ssa.Function, pa PanicArgs) {
	if _, ok := pa[fn]; ok {
		return
	}
	for i, fp := range fn.Params {
		if !isNillable(fp.Type()) {
			continue
		}
		if fp.Referrers() == nil {
			continue
		}
		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr,
				*ssa.IndexAddr,
				*ssa.MapUpdate:
				if !isNilChecked(fp, instr.Block()) {
					appendPA(pa, fn, i)
					break
				}
			case *ssa.Slice:
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && !isNilChecked(fp, instr.Block()) {
					appendPA(pa, fn, i)
					break
				}
			case *ssa.Store:
				if !isNilChecked(fp, instr.Block()) {
					appendPA(pa, fn, i)
					break
				}
			case *ssa.TypeAssert:
				if !isNilChecked(fp, instr.Block()) {
					appendPA(pa, fn, i)
					break
				}
			case *ssa.UnOp:
				if instr.Op == token.MUL && !isNilChecked(fp, instr.Block()) {
					appendPA(pa, fn, i)
					break
				}
			}
		}
	}
}

func appendPA(pa PanicArgs, fn *ssa.Function, i int) {
	if _, ok := pa[fn]; ok {
		pa[fn][i] = struct{}{}
		return
	}
	pa[fn] = map[int]struct{}{}
	pa[fn][i] = struct{}{}
}

func isNillable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Slice,
		*types.Interface,
		*types.Map,
		*types.Pointer:
		return true
	default:
		return false
	}
}

func isNilChecked(v *ssa.Parameter, b *ssa.BasicBlock) bool {
	bi := b.Idom()
	if bi == nil {
		return false
	}
	var binop *ssa.BinOp
	// IfInstruction is unique and last instruction if any in block.
	if If, ok := bi.Instrs[len(bi.Instrs)-1].(*ssa.If); ok {
		if binop, ok = If.Cond.(*ssa.BinOp); ok {
			switch binop.Op {
			case token.EQL:
				if isNil(binop.X) && binop.Y == v || isNil(binop.Y) && binop.X == v {
					return b == bi.Succs[1]
				}
			case token.NEQ:
				if isNil(binop.X) && binop.Y == v || isNil(binop.Y) && binop.X == v {
					return b == bi.Succs[0]
				}
			}
		}
	}
	return isNilChecked(v, b)
}

func isNil(v ssa.Value) bool {
	if v, ok := v.(*ssa.Const); ok {
		return v.IsNil()
	}
	return false
}
