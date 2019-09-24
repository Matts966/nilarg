package a // want package:"&{}"

import "bytes"

type X struct{ f, g int }

func f(i int, ip *int, x X, xp *X) { // want f:"&map\\[1:{} 3:{}\\]"
	print(i, *ip /* can be nil dereference */, x, *xp /* can be nil dereference */)
}

func f2(x *int, ptr *[3]int, i interface{}, m map[int]int) { // want f2:"&map\\[0:{} 1:{} 2:{} 3:{}\\]"
	// These can be nil dereferences.
	*x = 5
	print(ptr[:])
	print(i.(interface{ f() }))
	m[5] = 5
}

func f3(ptr *[3]int) { // want f3:"&map\\[0:{}\\]"
	// This can be a nil dereference.
	*ptr = [3]int{}
}

func f4(ptr *[3]int) {
	if ptr == nil {
		return
	}
	// These are not nil dereferences because of the nil check and an assignment.
	*ptr = [3]int{}
	print(*ptr)
}

func f5(x *int, ptr *[3]int, i interface{}, m map[int]int) {
	// These are not nil dereferences because of nil checks in previous lines.
	if x != nil {
		*x = 5
	}
	if ptr != nil {
		print(ptr[:])
		*ptr = [3]int{}
		print(*ptr)
	}
	if i != nil {
		print(i.(interface{ f() }))
	}
	if m != nil {
		m[5] = 5
	}
}

func f6(i interface{}) interface{ f() } {
	i2, ok := i.(interface{ f() })
	if ok {
		return i2
	}
	return nil
}

// f7 can also cause panic because f3 can.
func f7(ptr *[3]int) { // want f7:"&map\\[0:{}\\]"
	f3(ptr)
}

// f8 can also cause panic because f7 can.
func f8(ptr *[3]int) { // want f8:"&map\\[0:{}\\]"
	f7(ptr)
}

// f9 doesn't casuse panic because of nil check.
func f9(ptr *[3]int) {
	if ptr != nil {
		f7(ptr)
	}
}

// f10 can cause panic because Bytes does.
func f10(b *bytes.Buffer) { // want f10:"&map\\[0:{}\\]"
	b.Bytes()
}

func f11(i interface{}) interface{ f() } {
	if i != nil {
		if true {
			if true {
				return i.(interface{ f() })
			}
		}
	}
	return nil
}

type s struct {
	vars []*int
}
func (x *s) At(i int) *int { return x.vars[i] } // want At:"&map\\[0:{}\\]"
func f12(r *int, params *s) { // want f12:"&map\\[1:{}\\]"
	_ = params.At(1)
}
