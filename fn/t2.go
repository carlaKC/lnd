package fn

// T2 is the simplest 2-tuple type. It is useful for capturing ad hoc
// type conjunctions in a single value that can be easily dotchained.
type T2[A, B any] struct {
	fst A
	snd B
}

// Fst returns the first value in the T2.
func (t2 T2[A, B]) Fst() A {
	return t2.fst
}

// Snd returns the second value in the T2.
func (t2 T2[A, B]) Snd() B {
	return t2.snd
}

// AsGoPair ejects the 2-tuple's members into the multiple return values that
// are customary in go idiom.
func (t2 T2[A, B]) AsGoPair() (A, B) {
	return t2.fst, t2.snd
}
