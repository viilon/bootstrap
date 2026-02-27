package bootstrap

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// Helper types for testing
type (
	Config struct {
		Val string
	}

	Service struct {
		Cfg       *Config
		CleanedUp bool
	}

	App struct {
		Cfg *Config
		Svc *Service
	}

	Cleaner1 struct{}
	Cleaner2 struct{}

	FailingCleaner struct {
		Err error
	}
)

func (s *Service) Cleanup() error {
	s.CleanedUp = true
	return nil
}

func (c *FailingCleaner) Cleanup() error {
	return c.Err
}

// Global variables for order tracking (reset in test)
var (
	cleaner1Order *[]int
	cleaner2Order *[]int
)

func (c *Cleaner1) Cleanup() error {
	if cleaner1Order != nil {
		*cleaner1Order = append(*cleaner1Order, 1)
	}
	return nil
}

func (c *Cleaner2) Cleanup() error {
	if cleaner2Order != nil {
		*cleaner2Order = append(*cleaner2Order, 2)
	}
	return nil
}

func TestRunner(t *testing.T) {
	t.Run("Basic Execution Flow", func(t *testing.T) {
		r := New()
		var executed bool

		r.Add(
			func() *Config {
				return &Config{Val: "test"}
			},
			func(c *Config) *Service {
				return &Service{Cfg: c}
			},
			func(s *Service) {
				if s.Cfg.Val != "test" {
					t.Errorf("want test, got %s", s.Cfg.Val)
				}
				executed = true
			},
		)

		if err := r.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		if !executed {
			t.Error("final function not executed")
		}
	})

	t.Run("Context Injection", func(t *testing.T) {
		t.Run("Default Context", func(t *testing.T) {
			r := New()
			var ctx context.Context
			r.Add(func(c context.Context) {
				ctx = c
			})

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if ctx == nil {
				t.Error("context not injected")
			}
		})

		t.Run("WithContext Custom Value", func(t *testing.T) {
			key := "test_key"
			val := "test_val"
			baseCtx := context.WithValue(context.Background(), key, val)

			r := New().WithContext(baseCtx)
			var capturedCtx context.Context

			r.Add(func(ctx context.Context) {
				capturedCtx = ctx
			})

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if capturedCtx == nil {
				t.Fatal("context not injected")
			}
			if v := capturedCtx.Value(key); v != val {
				t.Errorf("context value mismatch, want %s, got %v", val, v)
			}

			// Verify Cleanup still works (cancel)
			if err := r.Cleanup(); err != nil {
				t.Fatalf("Cleanup failed: %v", err)
			}
			select {
			case <-capturedCtx.Done():
				// Success
			default:
				t.Error("context should be canceled after Cleanup")
			}
		})

		t.Run("Duplicate Context Provider", func(t *testing.T) {
			r := New()
			// New() already registers context.Context. Adding another should fail.
			r.Add(func() context.Context {
				return context.Background()
			})

			err := r.Run()
			if err == nil {
				t.Error("expected duplicate provider error, got nil")
			}
			if !strings.Contains(err.Error(), "duplicate provider") {
				t.Errorf("expected duplicate provider error, got: %v", err)
			}
		})
	})

	t.Run("Error Handling", func(t *testing.T) {
		t.Run("Init Error", func(t *testing.T) {
			r := New()
			expectedErr := errors.New("init failed")
			r.Add(func() error { return expectedErr })

			err := r.Run()
			if !errors.Is(err, expectedErr) {
				t.Errorf("want %v, got %v", expectedErr, err)
			}
		})

		t.Run("Error Position First", func(t *testing.T) {
			r := New()
			expectedErr := errors.New("init failed")
			r.Add(func() (error, *Config) { return expectedErr, nil })

			err := r.Run()
			if !errors.Is(err, expectedErr) {
				t.Errorf("want %v, got %v", expectedErr, err)
			}
		})

		t.Run("Error Position Middle", func(t *testing.T) {
			r := New()
			expectedErr := errors.New("init failed middle")
			r.Add(func() (*Config, error, *Service) { return &Config{}, expectedErr, nil })

			err := r.Run()
			if !errors.Is(err, expectedErr) {
				t.Errorf("want %v, got %v", expectedErr, err)
			}
		})

		t.Run("Delayed Add Error", func(t *testing.T) {
			r := New()
			r.Add("invalid") // First Add fails
			var executed bool
			r.Add(func() { executed = true }) // Second Add valid

			err := r.Run()
			if err == nil {
				t.Fatal("expected error from Run, got nil")
			}
			if !strings.Contains(err.Error(), "argument must be a function") {
				t.Errorf("unexpected error: %v", err)
			}
			if executed {
				t.Error("function executed despite previous Add error")
			}
		})
	})

	t.Run("Dependency Resolution", func(t *testing.T) {
		t.Run("Missing Dependency", func(t *testing.T) {
			r := New()
			r.Add(func(c *Config) {}) // Config missing

			err := r.Run()
			if err == nil {
				t.Error("expected error for missing dependency, got nil")
			}
		})

		t.Run("Circular Dependency", func(t *testing.T) {
			r := New()
			type A struct{}
			type B struct{}
			r.Add(
				func(b *B) *A { return &A{} },
				func(a *A) *B { return &B{} },
			)

			err := r.Run()
			if err == nil {
				t.Error("expected error for cycle, got nil")
			}
		})

		t.Run("Duplicate Providers (Deduplication)", func(t *testing.T) {
			r := New()
			count := 0
			provider := func() *Config {
				count++
				return &Config{Val: "dup"}
			}

			// Add the same provider twice
			r.Add(provider)
			r.Add(provider)

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if count != 1 {
				t.Errorf("provider executed %d times, want 1", count)
			}
		})
	})

	t.Run("Lifecycle", func(t *testing.T) {
		t.Run("Cleanup Basic", func(t *testing.T) {
			r := New()
			svc := &Service{}
			r.Add(func() *Service { return svc })

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}
			if svc.CleanedUp {
				t.Error("service cleaned up before Cleanup")
			}
			if err := r.Cleanup(); err != nil {
				t.Fatalf("Cleanup failed: %v", err)
			}
			if !svc.CleanedUp {
				t.Error("service not cleaned up after Cleanup")
			}
		})

		t.Run("Cleanup Order", func(t *testing.T) {
			r := New()
			var order []int
			cleaner1Order = &order
			cleaner2Order = &order
			// Clean up globals after test
			defer func() {
				cleaner1Order = nil
				cleaner2Order = nil
			}()

			r.Add(
				func() *Cleaner1 { return &Cleaner1{} },
				func(*Cleaner1) *Cleaner2 { return &Cleaner2{} },
			)

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			r.Cleanup()

			// Cleaner2 depends on Cleaner1 -> Created: 1 then 2 -> Cleaned up: 2 then 1
			if len(order) != 2 || order[0] != 2 || order[1] != 1 {
				t.Errorf("cleanup order wrong, want [2 1], got %v", order)
			}
		})

		t.Run("Cleanup Error Aggregation", func(t *testing.T) {
			r := New()
			expectedErr := errors.New("cleanup failed")
			r.Add(func() *FailingCleaner {
				return &FailingCleaner{Err: expectedErr}
			})

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			err := r.Cleanup()
			if err == nil {
				t.Fatal("expected error from Cleanup, got nil")
			}
			if !strings.Contains(err.Error(), "cleanup errors") || !strings.Contains(err.Error(), "cleanup failed") {
				t.Errorf("unexpected error message: %v", err)
			}
		})
	})

	t.Run("Field Injection Disabled", func(t *testing.T) {
		r := New()
		var app *App
		r.Add(
			func() *Config { return &Config{Val: "injected"} },
			func(c *Config) *Service { return &Service{Cfg: c} },
			func() *App { return &App{} }, // App.Cfg should NOT be injected
			func(a *App) { app = a },
		)

		if err := r.Run(); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		if app == nil {
			t.Fatal("App not injected")
		}
		if app.Cfg != nil || app.Svc != nil {
			t.Error("Struct fields should NOT be injected")
		}
	})

	t.Run("Population", func(t *testing.T) {
		t.Run("Basic Population", func(t *testing.T) {
			r := New()
			var cfg *Config
			r.Add(
				func() *Config { return &Config{Val: "populated"} },
				&cfg,
			)

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if cfg == nil {
				t.Fatal("config not populated")
			}
			if cfg.Val != "populated" {
				t.Errorf("want populated, got %s", cfg.Val)
			}
		})

		t.Run("Missing Dependency", func(t *testing.T) {
			r := New()
			var cfg *Config
			r.Add(&cfg)

			err := r.Run()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "missing dependency") {
				t.Errorf("unexpected error: %v", err)
			}
		})

		t.Run("Nil Pointer", func(t *testing.T) {
			r := New()
			// Passing nil pointer (not pointer to nil variable, but nil itself if interface, but here we pass typed nil?)
			// If we pass (*Config)(nil), that's a typed nil.
			// The check val.IsNil() handles it.
			r.Add((*Config)(nil))

			err := r.Run()
			// Actually Add should fail?
			// Wait, Add stores the error in b.err if add() returns error.
			// Let's check Add implementation.
			// if err := b.add(c); err != nil { b.err = err; return b }
			// So Run() will return that error.

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// But wait, Add returns *Bootstrap. The error is stored.
			// We should check if Run returns the error.
			if !strings.Contains(err.Error(), "argument must not be nil") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	})

	t.Run("Struct Injection", func(t *testing.T) {
		type NeedInject struct {
			Inject
			Cfg *Config
		}

		type NotInject struct {
			Cfg *Config
		}

		t.Run("Success", func(t *testing.T) {
			r := New()
			var ni NeedInject

			r.Add(
				func() *Config { return &Config{Val: "injected"} },
				&ni,
			)

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if ni.Cfg == nil {
				t.Fatal("Cfg not injected")
			}
			if ni.Cfg.Val != "injected" {
				t.Errorf("want injected, got %s", ni.Cfg.Val)
			}
		})

		t.Run("Struct Without Inject (Target Population)", func(t *testing.T) {
			r := New()
			var ni NotInject // No Inject embedded

			// This should now be treated as a target population request for 'NotInject' type.
			// Since we don't have a provider for NotInject, it should fail with missing dependency.
			r.Add(&ni)

			err := r.Run()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			// It should look for dependency of type 'NotInject'
			if !strings.Contains(err.Error(), "missing dependency") {
				t.Errorf("unexpected error: %v", err)
			}
		})

		t.Run("Struct Without Inject Success", func(t *testing.T) {
			r := New()
			var ni NotInject

			r.Add(
				func() NotInject { return NotInject{Cfg: &Config{Val: "direct"}} },
				&ni,
			)

			if err := r.Run(); err != nil {
				t.Fatalf("Run failed: %v", err)
			}

			if ni.Cfg == nil || ni.Cfg.Val != "direct" {
				t.Errorf("want direct, got %v", ni.Cfg)
			}
		})

		t.Run("Missing Dependency", func(t *testing.T) {
			r := New()
			var ni NeedInject
			r.Add(&ni)

			err := r.Run()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "missing dependency") {
				t.Errorf("unexpected error: %v", err)
			}
		})
	})
}

func TestIntegration_MixedInjection(t *testing.T) {
	// Scenario:
	// 1. Struct with embedded Inject -> Fields injected
	// 2. Struct without Inject -> Treated as dependency target (needs provider)
	// 3. Pointer to variable -> Populated with dependency

	type config struct {
		name string
	}

	type dbal struct {
		name string
	}

	type app struct {
		Inject
		Config *config
		DBAL   *dbal
	}

	var a app
	var c *config // Changed to pointer to match provider return type
	bc := New()

	// Register Providers
	bc.Add(func() *config {
		return &config{name: "config"}
	})
	bc.Add(func(c *config) *dbal {
		return &dbal{name: c.name}
	})

	// Register Injection Targets
	bc.Add(&a) // Struct Injection (has Inject)
	bc.Add(&c) // Variable Population (pointer to *config)

	err := bc.Run()
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	// Verify App (Struct Injection)
	if a.Config == nil || a.Config.name != "config" {
		t.Error("app.Config not injected correctly")
	}
	if a.DBAL == nil || a.DBAL.name != "config" {
		t.Error("app.DBAL not injected correctly")
	}

	// Verify Config Variable (Population)
	if c == nil || c.name != "config" {
		t.Error("c not populated correctly")
	}
}

func TestProhibitInjectEmbedInProvider(t *testing.T) {
	type Config struct {
		Val string
	}

	// Struct with Inject
	type Service struct {
		Inject
		Cfg *Config
	}

	t.Run("Provider Returns Struct With Inject (Output)", func(t *testing.T) {
		// Scenario: A provider returns *Service.
		// Expectation: The container prohibits this pattern.

		b := New()

		// Provider for Service (returns *Service)
		b.Add(func() *Service {
			return &Service{}
		})

		err := b.Run()
		if err == nil {
			t.Fatal("Expected error due to prohibited provider output, got nil")
		}
		if !strings.Contains(err.Error(), "prohibited") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})

	t.Run("Provider Requests Struct With Inject (Input)", func(t *testing.T) {
		// Scenario: A provider asks for *Service as input.
		// Expectation: The container prohibits this pattern.

		b := New()

		// Consumer asking for *Service
		b.Add(func(s *Service) int {
			return 1
		})

		err := b.Run()
		if err == nil {
			t.Fatal("Expected error due to prohibited provider input, got nil")
		}
		if !strings.Contains(err.Error(), "prohibited") {
			t.Errorf("Unexpected error message: %v", err)
		}
	})
}
