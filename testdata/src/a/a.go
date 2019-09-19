package a

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
