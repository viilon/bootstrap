package bootstrap

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"codeup.aliyun.com/viilon/project-x/foundation/bootstrap/dag"
)

// Bootstrap manages the bootstrap process with dependency injection and topological execution.
type Bootstrap struct {
	providers []*dag.Node
	values    map[reflect.Type]reflect.Value
	cleanups  []func() error
	functions map[uintptr]bool // Cache for registered functions to avoid duplicates
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.RWMutex
	err       error // Store the first error encountered during Add
}

// New creates a new Bootstrap.
func New() *Bootstrap {
	ctx, cancel := context.WithCancel(context.Background())
	r := &Bootstrap{
		providers: make([]*dag.Node, 0),
		values:    make(map[reflect.Type]reflect.Value),
		cleanups:  make([]func() error, 0),
		functions: make(map[uintptr]bool),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Register default context provider
	r.Add(func() context.Context {
		return r.ctx
	})

	return r
}

// WithContext sets a custom context for the runner.
// It wraps the provided context with cancellation support, allowing Cleanup() to still work.
// This method is thread-safe and can be chained.
func (b *Bootstrap) WithContext(ctx context.Context) *Bootstrap {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Wrap the provided context with cancellation
	c, cancel := context.WithCancel(ctx)
	b.ctx = c
	b.cancel = cancel
	return b
}

// Add registers one or more constructors.
func (b *Bootstrap) Add(constructors ...interface{}) *Bootstrap {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.err != nil {
		return b
	}

	for _, c := range constructors {
		if err := b.add(c); err != nil {
			b.err = err
			return b
		}
	}
	return b
}

// Run executes all registered constructors in topological order.
func (b *Bootstrap) Run() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.err != nil {
		return b.err
	}

	sorted, err := dag.Resolve(b.providers)
	if err != nil {
		return err
	}

	// Execute
	for _, p := range sorted {
		if err := b.execute(p); err != nil {
			return err
		}
	}

	return nil
}

// Cleanup gracefully shuts down the runner by calling registered cleanups in reverse order.
func (b *Bootstrap) Cleanup() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Cancel the context first
	b.cancel()

	var errs []error
	// Execute cleanups in reverse order
	for i := len(b.cleanups) - 1; i >= 0; i-- {
		if err := b.cleanups[i](); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}
	return nil
}
func (b *Bootstrap) add(fn interface{}) error {
	val := reflect.ValueOf(fn)
	typ := val.Type()

	if typ.Kind() == reflect.Func {
		return b.registerProvider(fn)
	}

	if typ.Kind() == reflect.Ptr {
		if val.IsNil() {
			return fmt.Errorf("argument must not be nil")
		}

		elemType := typ.Elem()

		// Case 1: Struct Injection (must embed bootstrap.Inject)
		if elemType.Kind() == reflect.Struct {
			if hasInject(elemType) {
				return b.registerStructInjector(val)
			}
			// If not embedded Inject, fall through to Target Population
		}

		// Case 2: Target Population (pointer to pointer or interface, OR struct without Inject)
		return b.registerTargetPopulator(val)
	}

	return fmt.Errorf("argument must be a function or pointer")
}

func (b *Bootstrap) registerProvider(fn interface{}) error {
	val := reflect.ValueOf(fn)
	typ := val.Type()

	// Check inputs for embedded Inject
	if err := checkInjectInTypes(typ.NumIn(), typ.In, "input"); err != nil {
		return err
	}

	// Check outputs for embedded Inject
	if err := checkInjectInTypes(typ.NumOut(), typ.Out, "output"); err != nil {
		return err
	}

	ptr := val.Pointer()
	if b.functions[ptr] {
		return nil // Already registered
	}
	b.functions[ptr] = true

	p, err := dag.NewNode(fn)
	if err != nil {
		return err
	}
	b.providers = append(b.providers, p)
	return nil
}

func (b *Bootstrap) registerTargetPopulator(ptrVal reflect.Value) error {
	// ptrVal is *T. We want to set it to a value of type T.
	targetType := ptrVal.Type().Elem()

	// Create synthetic function: func(v T)
	fnType := reflect.FuncOf([]reflect.Type{targetType}, nil, false)
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		ptrVal.Elem().Set(args[0])
		return nil
	})

	// Register as provider
	// We don't check for duplicates here as multiple variables might need same value?
	// But b.add calls check duplicates based on function pointer.
	// reflect.MakeFunc creates unique pointers? Let's assume so or checks handle it.
	// Actually, we should probably bypass b.functions check for synthetic nodes,
	// or ensure they are unique.
	// For now, let's just use common logic but maybe synthetic functions have unique pointers.

	p, err := dag.NewNode(fn.Interface())
	if err != nil {
		return err
	}
	b.providers = append(b.providers, p)
	return nil
}

func (b *Bootstrap) registerStructInjector(structPtrVal reflect.Value) error {
	// structPtrVal is *Struct.
	structType := structPtrVal.Type().Elem()

	var fieldTypes []reflect.Type
	var fieldIndices []int

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)
		// Skip unexported fields? Usually yes.
		if field.PkgPath != "" {
			continue
		}
		// Skip the Inject field itself?
		if field.Type == reflect.TypeOf(Inject{}) {
			continue
		}

		fieldTypes = append(fieldTypes, field.Type)
		fieldIndices = append(fieldIndices, i)
	}

	// Create synthetic function: func(f1 T1, f2 T2, ...)
	fnType := reflect.FuncOf(fieldTypes, nil, false)
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		elem := structPtrVal.Elem()
		for i, arg := range args {
			elem.Field(fieldIndices[i]).Set(arg)
		}
		return nil
	})

	p, err := dag.NewNode(fn.Interface())
	if err != nil {
		return err
	}
	b.providers = append(b.providers, p)
	return nil
}

func hasInject(typ reflect.Type) bool {
	injectType := reflect.TypeOf(Inject{})
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Anonymous && field.Type == injectType {
			return true
		}
	}
	return false
}

func checkInjectInTypes(count int, getType func(int) reflect.Type, kindStr string) error {
	for i := 0; i < count; i++ {
		t := getType(i)
		checkType := t
		if checkType.Kind() == reflect.Ptr {
			checkType = checkType.Elem()
		}
		if checkType.Kind() == reflect.Struct && hasInject(checkType) {
			return fmt.Errorf("provider %s type %v embeds bootstrap.Inject, which is prohibited", kindStr, t)
		}
	}
	return nil
}
func (b *Bootstrap) execute(p *dag.Node) error {
	var args []reflect.Value
	args = make([]reflect.Value, len(p.Inputs))

	for i, in := range p.Inputs {
		if val, ok := b.values[in]; ok {
			args[i] = val
		} else {
			return fmt.Errorf("internal error: missing value for type %v", in)
		}
	}

	results := p.Fn.Call(args)

	// Check error returns
	for _, idx := range p.ErrorIndices {
		errVal := results[idx]
		if !errVal.IsNil() {
			return errVal.Interface().(error)
		}
	}

	// Store results (excluding errors) and register cleanups
	// Note: p.outputs corresponds to results excluding errors, BUT
	// results contains EVERYTHING including errors at specific indices.
	// We need to map outputs to results carefully.

	outputIdx := 0
	for i, res := range results {
		// Check if this index was an error index
		isError := false
		for _, errIdx := range p.ErrorIndices {
			if i == errIdx {
				isError = true
				break
			}
		}
		if isError {
			continue
		}

		if outputIdx < len(p.Outputs) {
			outType := p.Outputs[outputIdx]
			outputIdx++

			// Store in values map
			b.values[outType] = res

			// Register Cleanup
			if res.IsValid() {
				// Check for nil only on nillable types to avoid panic
				isNil := false
				switch res.Kind() {
				case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
					if res.IsNil() {
						isNil = true
					}
				}

				if !isNil {
					if cleanable, ok := res.Interface().(Cleanable); ok {
						b.cleanups = append(b.cleanups, cleanable.Cleanup)
					}
				}
			}
		}
	}

	return nil
}
