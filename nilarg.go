package nilarg

import (
	"go/token"
	"go/types"
	"math/big"
	"reflect"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/buildssa"
	"golang.org/x/tools/go/ssa"
)

const Doc = `check for arguments that cause panic when they are nil

The nilarg checker finds arguments that can be nil and cause panic in
function when they are nil.

The conditions are such as:
	f(x *int) { *x }
and:
	f(m map[int]int) { map[5] = 5 }
and:
	f(i interface{}) { i.(interface{ f() }) }

These codes do not always cause panic, but panic if the argument is nil.
Also the nilarg checker reports some false positive cases when the
instructions that refer the arguments are not reachable.
`

var Analyzer = &analysis.Analyzer{
	Name:      "nilarg",
	Doc:       Doc,
	Run:       run,
	Requires:  []*analysis.Analyzer{buildssa.Analyzer},
	FactTypes: []analysis.Fact{new(panicArgs), new(pkgDone)},
}

// panicArgs has the information about arguments which causes panic on
// calling the function when it is nil.
type panicArgs map[int]struct{}

func (*panicArgs) AFact() {}

type pkgDone struct{}

func (*pkgDone) AFact() {}

func run(pass *analysis.Pass) (interface{}, error) {
	ssainput := pass.ResultOf[buildssa.Analyzer].(*buildssa.SSA)
	for {
		cc := 0
		for _, fn := range ssainput.SrcFuncs {
			if changed := checkFunc(pass, fn); changed {
				cc++
			}
		}
		if cc == 0 {
			pass.ExportPackageFact(&pkgDone{})
			break
		}
	}

	// Push the information about nilness of values like nilness and
	// if calls are called with nil value and they can cause panic
	// with nil arguments, report the call.
	for _, fn := range ssainput.SrcFuncs {
		runFunc(pass, fn)
	}

	return nil, nil
}

// This function checkFunc checks all the nillable type arguments of
// the function fn and instructions in fn that refer the arguments.
// If those instructions cause panic when the referred argument is nil,
// then this function exports the information as the ObjectFact of fn
// using panicArgs type.
func checkFunc(pass *analysis.Pass, fn *ssa.Function) bool {
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
			start := big.NewInt(0)
			switch instr := fpr.(type) {
			case ssa.CallInstruction:
				if !instr.Common().IsInvoke() {
					ffact := panicArgs{}
					if instr.Common().StaticCallee() == nil || instr.Common().StaticCallee().Object() == nil {
						// a builtin or dynamically dispatched function call
						continue
					}
					f := instr.Common().StaticCallee().Object()
					if f.Pkg() != pass.Pkg {
						if !pass.ImportPackageFact(f.Pkg(), &pkgDone{}) {
							// not changed but can change later
							return true
						}
						if pass.ImportObjectFact(f, &ffact) {
							for fi := range ffact {

								if i >= len(instr.Common().Args) {
									continue
								}

								if instr.Common().Args[fi] == fp && !isNilChecked(fp, instr.Block(), start) {
									fact[i] = struct{}{}
									break refLoop
								}
							}
						}
					}
					if pass.ImportObjectFact(f, &ffact) {
						for fi := range ffact {

							if i >= len(instr.Common().Args) {
								continue
							}

							if instr.Common().Args[fi] == fp && !isNilChecked(fp, instr.Block(), start) {
								fact[i] = struct{}{}
								break refLoop
							}
						}
					}
				}
			case *ssa.FieldAddr:
				// the address of fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Field:
				// fp.field
				if instr.X == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.IndexAddr:
				// fp[i]
				if instr.X == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.TypeAssert:
				// Only the 1-result type assertion panics.
				//
				// _ = fp.(someType)
				if instr.X == fp && !instr.CommaOk && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Slice:
				// Slice operation to a pointer fp cause nil pointer
				// dereference iff fp is nil.
				//
				// fp[:]
				if _, ok := instr.X.Type().Underlying().(*types.Pointer); ok && instr.X == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.Store:
				// *fp = v
				if instr.Addr == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.MapUpdate:
				// *fp[x] = y
				if instr.Map == fp && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			case *ssa.UnOp:
				// *fp
				if instr.X == fp && instr.Op == token.MUL && !isNilChecked(fp, instr.Block(), start) {
					fact[i] = struct{}{}
					break refLoop
				}
			}
		}
	}
	// If no argument cause panic, skip exporting the fact.
	if len(fact) > 0 && fn.Object() != nil {
		var oldFact panicArgs
		if pass.ImportObjectFact(fn.Object(), &oldFact) && !reflect.DeepEqual(oldFact, fact) {
			pass.ExportObjectFact(fn.Object(), &fact)
			return true
		}
		pass.ExportObjectFact(fn.Object(), &fact)
	}
	return false
}

// isNillable returns true when the values of t can be nil
// and cause nil pointer dereference.
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

// isNilChecked reports whether block b is dominated by a check
// of the condition v != nil.
func isNilChecked(v *ssa.Parameter, b *ssa.BasicBlock, visited *big.Int) bool {
	vis := big.NewInt(1)
	vis.Lsh(vis, uint(b.Index))
	if vis.Or(visited, vis) == visited {
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
	visited = vis
	return isNilChecked(v, bi, visited)
}

// isNil returns true when the value is a constant nil.
func isNil(value ssa.Value) bool {
	v, ok := value.(*ssa.Const)
	return ok && v.IsNil()
}

func runFunc(pass *analysis.Pass, fn *ssa.Function) {
	seen := make([]bool, len(fn.Blocks))
	var visit func(b *ssa.BasicBlock, stack []fact)
	visit = func(b *ssa.BasicBlock, stack []fact) {
		if seen[b.Index] {
			return
		}
		seen[b.Index] = true

		// Report calls that can cause panic.
		for _, instr := range b.Instrs {
			if c, ok := instr.(*ssa.Call); ok {
				s := c.Call.StaticCallee()
				if s == nil || s.Object() == nil {
					continue
				}
				var fact panicArgs
				if pass.ImportObjectFact(s.Object(), &fact) {
					for i := range fact {

						if i >= len(c.Common().Args) {
							continue
						}

						if nilnessOf(stack, c.Common().Args[i]) == isnil {
							pass.Reportf(c.Pos(), "this call can cause panic")
						}
					}
				}
			}
		}

		// For nil comparison blocks, report an error if the condition
		// is degenerate, and push a nilness fact on the stack when
		// visiting its true and false successor blocks.
		if binop, tsucc, fsucc := eq(b); binop != nil {
			xnil := nilnessOf(stack, binop.X)
			ynil := nilnessOf(stack, binop.Y)
			if ynil != unknown && xnil != unknown && (xnil == isnil || ynil == isnil) {
				// If tsucc's or fsucc's sole incoming edge is impossible,
				// it is unreachable.  Prune traversal of it and
				// all the blocks it dominates.
				// (We could be more precise with full dataflow
				// analysis of control-flow joins.)
				var skip *ssa.BasicBlock
				if xnil == ynil {
					skip = fsucc
				} else {
					skip = tsucc
				}
				for _, d := range b.Dominees() {
					if d == skip && len(d.Preds) == 1 {
						continue
					}
					visit(d, stack)
				}
				return
			}

			// "if x == nil" or "if nil == y" condition; x, y are unknown.
			if xnil == isnil || ynil == isnil {
				var f fact
				if xnil == isnil {
					// x is nil, y is unknown:
					// t successor learns y is nil.
					f = fact{binop.Y, isnil}
				} else {
					// x is nil, y is unknown:
					// t successor learns x is nil.
					f = fact{binop.X, isnil}
				}

				for _, d := range b.Dominees() {
					// Successor blocks learn a fact
					// only at non-critical edges.
					// (We could do be more precise with full dataflow
					// analysis of control-flow joins.)
					s := stack
					if len(d.Preds) == 1 {
						if d == tsucc {
							s = append(s, f)
						} else if d == fsucc {
							s = append(s, f.negate())
						}
					}
					visit(d, s)
				}
				return
			}
		}

		for _, d := range b.Dominees() {
			visit(d, stack)
		}
	}

	if fn.Blocks != nil {
		visit(fn.Blocks[0], make([]fact, 0, 20)) // 20 is plenty
	}
}

// A fact records that a block is dominated
// by the condition v == nil or v != nil.
type fact struct {
	value   ssa.Value
	nilness nilness
}

func (f fact) negate() fact { return fact{f.value, -f.nilness} }

type nilness int

const (
	isnonnil         = -1
	unknown  nilness = 0
	isnil            = 1
)

var nilnessStrings = []string{"non-nil", "unknown", "nil"}

func (n nilness) String() string { return nilnessStrings[n+1] }

// nilnessOf reports whether v is definitely nil, definitely not nil,
// or unknown given the dominating stack of facts.
func nilnessOf(stack []fact, v ssa.Value) nilness {
	// Is value intrinsically nil or non-nil?
	switch v := v.(type) {
	case *ssa.Alloc,
		*ssa.FieldAddr,
		*ssa.FreeVar,
		*ssa.Function,
		*ssa.Global,
		*ssa.IndexAddr,
		*ssa.MakeChan,
		*ssa.MakeClosure,
		*ssa.MakeInterface,
		*ssa.MakeMap,
		*ssa.MakeSlice:
		return isnonnil
	case *ssa.Const:
		if v.IsNil() {
			return isnil
		} else {
			return isnonnil
		}
	}

	// Search dominating control-flow facts.
	for _, f := range stack {
		if f.value == v {
			return f.nilness
		}
	}
	return unknown
}

// If b ends with an equality comparison, eq returns the operation and
// its true (equal) and false (not equal) successors.
func eq(b *ssa.BasicBlock) (op *ssa.BinOp, tsucc, fsucc *ssa.BasicBlock) {
	if If, ok := b.Instrs[len(b.Instrs)-1].(*ssa.If); ok {
		if binop, ok := If.Cond.(*ssa.BinOp); ok {
			switch binop.Op {
			case token.EQL:
				return binop, b.Succs[0], b.Succs[1]
			case token.NEQ:
				return binop, b.Succs[1], b.Succs[0]
			}
		}
	}
	return nil, nil, nil
}
