package nilarg

import (
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

const Doc = `check for arguments that cause panic when they are nil

The nilarg checker finds arguments that can be nil and cause panic in
function when they are nil and export their information as Object Facts.

The conditions that export Fact are such as:
	f(x *int) { *x }
and:
	f(m map[int]int) { map[5] = 5 }
and:
	f(i interface{}) { i.(interface{ f() }) }

These codes do not always cause panic, but panic if the argument is nil.
Also the nilarg checker exports some false positive cases when the
instructions that refer the arguments are not reachable.
`

var Analyzer = &analysis.Analyzer{
	Name:      "nilarg",
	Doc:       Doc,
	Run:       run,
	Requires:  []*analysis.Analyzer{buildssa.Analyzer},
	FactTypes: []analysis.Fact{new(panicArgs)},
}

// panicArgs has the information about arguments which causes panic on
// calling the function when it is nil.
type panicArgs map[int]struct{}

func (*panicArgs) AFact() {}

func run(pass *analysis.Pass) (interface{}, error) {
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for _, fn := range ssainput.SrcFuncs {
		checkFunc(pass, fn)
	}
	return nil, nil
}

// This function checkFunc checks all the nillable type arguments of
// the function fn and instructions in fn that refer the arguments.
// If those instructions cause panic when the referred argument is nil,
// then this function exports the information as the Object Fact of fn
// using panicArgs type.
func checkFunc(pass *analysis.Pass, fn *ssa.Function) {
	fact := panicArgs{}
	for i, fp := range fn.Params {
		// If the argument fp can't be nil or there are no referrers
		// of fp in fn, skip check.
		if !isNillable(fp.Type()) {
			continue
		}
		if fp.Referrers() == nil {
			continue
		}

	refLoop:
		// Check all the referrers and if the instruction cause panic when
		// fp is nil, add fact of it and break this loop.
		for _, fpr := range *fp.Referrers() {
			switch instr := fpr.(type) {
			case *ssa.FieldAddr:
				// the address of fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Field:
				// fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.IndexAddr:
				// fp[i]
				if instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.TypeAssert:
				// Only the 1-result type assertion panics.
				//
				// _ = fp.(someType)
				if instr.X == fp && !instr.CommaOk && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Slice:
				// Slice operation to a pointer fp cause nil pointer
				// dereference iff fp is nil.
				//
				// fp[:]
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Store:
				// *fp = v
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.MapUpdate:
				// *fp[x] = y
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.UnOp:
				// *fp
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), nil) {
					fact[i] = struct{}{}
					break refLoop
				}
			}
		}
	}
	// If no argument cause panic, skip exporting the fact.
	if len(fact) > 0 && fn.Object() != nil {
		pass.ExportObjectFact(fn.Object(), &fact)
	}
}

// isNillable returns true when the values of t can be nil.
func isNillable(t types.Type) bool {
	switch t.Underlying().(type) {
	case *types.Slice,
		*types.Interface,
		*types.Map,
		*types.Chan,
		*types.Pointer:
		return true
	default:
		return false
	}
}

// isNilChecked reports whether block b is dominated by a check
// of the condition v != nil.
func isNilChecked(v *ssa.Parameter, b *ssa.BasicBlock, visited map[*ssa.BasicBlock]struct{}) bool {
	if _, ok := visited[b]; ok {
		return false
	}
	// We could be more precise with full dataflow
	// analysis of control-flow joins.
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
	visited[b] = struct{}{}
	return isNilChecked(v, b, visited)
}

// isNil returns true when the value is a constant nil.
func isNil(value ssa.Value) bool {
	v, ok := value.(*ssa.Const)
	return ok && v.IsNil()
}
