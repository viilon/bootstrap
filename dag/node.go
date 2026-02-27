package dag

import (
	"fmt"
	"reflect"
)

// Node holds reflection information about a constructor.
type Node struct {
	Fn           reflect.Value
	Inputs       []reflect.Type
	Outputs      []reflect.Type
	ErrorIndices []int // indices of return values that are errors
}

func NewNode(fn interface{}) (*Node, error) {
	val := reflect.ValueOf(fn)
	typ := val.Type()

	if typ.Kind() != reflect.Func {
		return nil, fmt.Errorf("runner: argument must be a function, got %v", typ)
	}

	n := &Node{
		Fn:           val,
		Inputs:       make([]reflect.Type, 0),
		Outputs:      make([]reflect.Type, 0),
		ErrorIndices: make([]int, 0),
	}

	// Analyze inputs
	for i := 0; i < typ.NumIn(); i++ {
		n.Inputs = append(n.Inputs, typ.In(i))
	}

	// Analyze outputs
	for i := 0; i < typ.NumOut(); i++ {
		outTyp := typ.Out(i)
		// Check if the return value is error
		if outTyp.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			n.ErrorIndices = append(n.ErrorIndices, i)
			continue
		}
		n.Outputs = append(n.Outputs, outTyp)
	}

	return n, nil
}
