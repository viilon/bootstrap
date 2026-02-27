# Bootstrap

Bootstrap is a lightweight Go dependency injection container and application bootstrapper. It simplifies application initialization by automatically managing component dependencies, startup order, and lifecycle resource cleanup.

> ⚠️ **Important**: This package is designed exclusively for the **project initialization phase** (e.g., in `main` functions or startup scripts). Do NOT use it in runtime logic (such as HTTP request handling or background tasks) to retrieve dependencies or manage objects. Doing so violates the principles of dependency injection and introduces unnecessary performance overhead.

[中文文档](README_zh.md)

## ✨ Features

*   **Auto Dependency Injection**: Automatically wires component dependencies based on constructor arguments and return values using reflection. Supports constructor injection, struct field injection, and variable population.
*   **Topological Startup**: Uses a DAG (Directed Acyclic Graph) algorithm to calculate the startup order, ensuring dependencies are initialized before dependents. This applies to all injection methods.
*   **Lifecycle Management**: Automatically identifies and manages components implementing the `Cleanable` interface, supporting graceful shutdown.
*   **Cycle Detection**: Detects and reports circular dependencies during the startup phase to prevent runtime deadlocks.
*   **Context Propagation**: Built-in `context.Context` support that propagates throughout the entire initialization and cleanup process.

## 🚀 Usage

### 1. Define Components

Write standard Go functions as constructors (Providers). Constructors can declare dependencies as arguments and return component instances along with an optional error.

```go
type Config struct {
    Port int
}

func NewConfig() *Config {
    return &Config{Port: 8080}
}

type Database struct {
    Url string
}

// Database implements the Cleanable interface
func (db *Database) Cleanup() error {
    fmt.Println("Closing database...")
    return nil
}

// NewDatabase depends on *Config
func NewDatabase(cfg *Config) (*Database, error) {
    return &Database{Url: fmt.Sprintf("db://localhost:%d", cfg.Port)}, nil
}

type Server struct {
    Db *Database
}

// NewServer depends on *Database
func NewServer(db *Database) *Server {
    return &Server{Db: db}
}
```

### 2. Struct Injection & Population

You can inject dependencies into variables or struct fields by passing pointers to `Add`.

**Scenario A: Struct Field Injection**
Embed `bootstrap.Inject` in your struct to automatically inject all *exported* fields.

> ⚠️ **Restriction**: Structs embedding `bootstrap.Inject` cannot be used as input arguments or return values in provider functions. They are only supported for direct injection via `Add(&struct)`.

```go
type Application struct {
    bootstrap.Inject // Required for field injection
    
    Cfg *Config    // Will be injected
    Svc *Service   // Will be injected
    // db *Database // Private fields are IGNORED
}
```

**Scenario B: Variable Population**
Pass a pointer to any variable (including a struct without `bootstrap.Inject`), and the container will populate it with the corresponding dependency.

```go
var cfg *Config // Will be populated
var db *Database // Will be populated
```

### 3. Bootstrap the Application

Assemble and start the container in your `main` function.

```go
func main() {
    // 1. Create container
    app := bootstrap.New()
    
    // Define variables to hold instances
    var appStruct Application
    var simpleCfg *Config

    // 2. Register constructors and targets
    app.Add(
        NewConfig,
        NewDatabase,
        NewServer,
        
        // Scenario A: Struct Injection
        // Fields of appStruct will be populated
        &appStruct, 
        
        // Scenario B: Variable Population
        // simpleCfg will be set to the instance of *Config
        &simpleCfg,
        
        // Anonymous functions
        func(s *Server) {
            fmt.Println("Server started with DB:", s.Db.Url)
        },
    )

    // 3. Run initialization
    if err := app.Run(); err != nil {
        panic(err)
    }
    
    // Now variables are populated
    fmt.Println("Config port:", appStruct.Cfg.Port)
    fmt.Println("Simple Config port:", simpleCfg.Port)

    // 4. Graceful Shutdown
    // Execute resource cleanup before program exit (e.g., on SIGTERM)
    defer func() {
        if err := app.Cleanup(); err != nil {
            fmt.Printf("Cleanup errors: %v\n", err)
        }
    }()
    
    // Block main process...
    select {}
}
```

## 📦 Interfaces

### Cleanable Interface

If your component needs to release resources when the application stops (e.g., closing DB connections, stopping background goroutines), implement the `Cleanable` interface:

```go
type Cleanable interface {
    Cleanup() error
}
```

`bootstrap` automatically collects all instances implementing this interface and executes their `Cleanup` methods in **reverse initialization order** when `app.Cleanup()` is called.

## 💡 Scenarios

*   **Application Entry (Main)**: Replaces messy manual initialization code (`repo := NewRepo(db); svc := NewService(repo)...`), keeping `main` clean.
*   **Modular Development**: Modules only need to expose constructors without worrying about specific instantiation timing.
*   **Testing**: Quickly assemble required dependency graphs in unit or integration tests.

## ⚠️ Caveats

1.  **Initialization Only**: Do not pass the container to the business logic layer or dynamically add/retrieve dependencies at runtime. All dependencies should be explicitly declared via constructor arguments at startup.
2.  **Unique Type Constraint**: Within a single container, **there can be only one Provider per return type**. For example, you cannot have two functions both returning `*sql.DB`, as the container won't know which one to inject. Use different struct types or type aliases to distinguish them.
3.  **Reflection Overhead**: This package uses reflection during the startup phase to analyze dependencies. While the overhead is minimal and only occurs once at startup, evaluate this if your application requires ultra-fast cold starts.
4.  **Error Handling**: Constructors can return an `error` as their last return value. If any constructor returns a non-nil error, `Run()` will terminate immediately and return that error.
5.  **Not Thread-Safe**: While `Add` and `Run` have lock protection, they are designed primarily for the single-threaded initialization flow.

## 🛠 Internals

1.  **Graph Construction**:
    *   `bootstrap` iterates through all registered constructors, analyzing their input parameters (dependencies) and output parameters (productions) via `reflect`.
    *   This information is built into a dependency graph.

2.  **Topological Sort**:
    *   Performs a topological sort on the dependency graph using Depth-First Search (DFS).
    *   Detects cycles (A->B->A) during this process and reports errors immediately.

3.  **Sequential Execution**:
    *   Invokes constructors sequentially based on the sorted order.
    *   Results are cached in a `values` map for injection into subsequent components.

4.  **Resource Management**:
    *   After a constructor executes successfully, its returned instance is checked for the `Cleanable` interface implementation.
    *   If implemented, its `Cleanup` method is added to a cleanup list.
    *   `Cleanup()` executes these methods in reverse order, ensuring low-level resources (like DBs) are closed after the resources that depend on them (like Services).
