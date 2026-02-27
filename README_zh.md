# Bootstrap

Bootstrap 是一个轻量级的 Go 语言依赖注入（Dependency Injection）容器和应用启动器。它旨在简化应用程序的初始化过程，自动管理组件之间的依赖关系、启动顺序以及生命周期资源回收。

> ⚠️ **重要提示**：本包仅设计用于**项目初始化阶段**（`main` 函数或启动脚本中）。请勿在项目启动后的运行时逻辑中（如 HTTP 请求处理、定时任务等）使用它来获取依赖或管理对象，这会违背依赖注入的设计初衷并带来不必要的性能开销。

## ✨ 核心特性

*   **自动依赖注入**：基于反射分析构造函数的参数和返回值，自动装配组件依赖。支持构造函数注入、结构体字段注入和变量自动填充。
*   **拓扑排序启动**：使用 DAG（有向无环图）算法计算启动顺序，确保被依赖的组件先初始化。此机制适用于所有注入方式。
*   **生命周期管理**：自动识别并管理实现了 `Cleanable` 接口的组件，支持优雅关闭（Graceful Shutdown）。
*   **循环依赖检测**：在启动阶段检测并报告循环依赖，防止运行时死锁。
*   **上下文传播**：内置 `context.Context` 支持，贯穿整个初始化和清理过程。

## 🚀 使用方法

### 1. 定义组件

编写普通的 Go 函数作为构造函数（Provider）。构造函数可以声明依赖参数，并返回组件实例和可选的错误。

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

// Database 实现了 Cleanable 接口
func (db *Database) Cleanup() error {
    fmt.Println("Closing database...")
    return nil
}

// NewDatabase 依赖 *Config
func NewDatabase(cfg *Config) (*Database, error) {
    return &Database{Url: fmt.Sprintf("db://localhost:%d", cfg.Port)}, nil
}

type Server struct {
    Db *Database
}

// NewServer 依赖 *Database
func NewServer(db *Database) *Server {
    return &Server{Db: db}
}
```

### 2. 结构体注入与变量填充

你可以通过将指针传递给 `Add` 方法来将依赖注入到变量或结构体字段中。

**场景 A: 结构体字段注入**
在结构体中内嵌 `bootstrap.Inject`，容器会自动注入该结构体中所有**导出**的字段。

> ⚠️ **限制**：内嵌 `bootstrap.Inject` 的结构体不能作为 provider 函数的入参或返回值。它们仅支持通过 `Add(&struct)` 进行直接注入。

```go
type Application struct {
    bootstrap.Inject // 必须内嵌此字段以启用字段注入
    
    Cfg *Config    // 将被注入
    Svc *Service   // 将被注入
    // db *Database // 私有字段会被忽略
}
```

**场景 B: 变量填充**
传入任何变量的指针（包括未内嵌 `bootstrap.Inject` 的结构体），容器会将对应的依赖实例填充到该变量中。

```go
var cfg *Config // 将被填充
var db *Database // 将被填充
```

### 3. 启动应用

在 `main` 函数中使用 `bootstrap` 容器进行组装和启动。

```go
func main() {
    // 1. 创建容器
    app := bootstrap.New()
    
    // 定义变量用于接收实例
    var appStruct Application
    var simpleCfg *Config

    // 2. 注册构造函数和注入目标
    app.Add(
        NewConfig,
        NewDatabase,
        NewServer,
        
        // 场景 A: 结构体注入
        // appStruct 的导出字段将被填充
        &appStruct,
        
        // 场景 B: 变量填充
        // simpleCfg 将被赋值为 *Config 类型的实例
        &simpleCfg,
        
        // 匿名函数作为启动钩子
        func(s *Server) {
            fmt.Println("Server started with DB:", s.Db.Url)
        },
    )

    // 3. 运行初始化
    if err := app.Run(); err != nil {
        panic(err)
    }
    
    // 此时变量已经被赋值
    fmt.Println("Config port:", appStruct.Cfg.Port)
    fmt.Println("Simple Config port:", simpleCfg.Port)

    // 4. 优雅退出处理
    // 在程序退出前（如收到 SIGTERM 信号），执行资源清理
    defer func() {
        if err := app.Cleanup(); err != nil {
            fmt.Printf("Cleanup errors: %v\n", err)
        }
    }()
    
    // 阻塞主进程...
    select {}
}
```

## 📦 接口说明

### Cleanable 接口

如果你的组件需要在应用停止时释放资源（如关闭数据库连接、停止后台协程），请实现 `Cleanable` 接口：

```go
type Cleanable interface {
    Cleanup() error
}
```

`bootstrap` 会自动收集所有实现了该接口的实例，并在调用 `app.Cleanup()` 时按**初始化顺序的逆序**执行 `Cleanup` 方法。

## 💡 使用场景

*   **应用程序入口 (Main)**：替代繁琐的手动初始化代码（`repo := NewRepo(db); svc := NewService(repo)...`），让 `main` 函数更整洁。
*   **模块化开发**：各模块只需暴露构造函数，无需关心具体的实例化时机。
*   **测试环境搭建**：在单元测试或集成测试中，快速组装所需的依赖图。

## ⚠️ 注意事项

1.  **仅限初始化使用**：不要将容器传递给业务逻辑层，或者在运行时动态添加/获取依赖。所有依赖关系应在启动时通过构造函数参数明确声明。
2.  **唯一类型限制**：在同一个容器中，**每种类型的返回值只能有一个 Provider**。例如，不能有两个函数都返回 `*sql.DB`，否则容器无法确定注入哪一个。建议使用不同的结构体类型或类型别名来区分。
3.  **反射开销**：该包在启动阶段使用了反射（Reflection）来分析依赖。虽然开销很小且只发生在启动时，但在对启动速度有极致要求的场景下需评估。
4.  **Error 处理**：构造函数可以返回 `error` 作为最后一个返回值。如果任何一个构造函数返回非 nil 错误，`Run()` 过程将立即终止并返回该错误。
5.  **非线程安全**：`Add` 和 `Run` 方法虽然有锁保护，但设计上主要用于单线程的初始化流程。

## 🛠 实现原理

1.  **依赖图构建**：
    *   `bootstrap` 遍历所有注册的构造函数，通过 `reflect` 分析其输入参数（依赖）和输出参数（产出）。
    *   将这些信息构建成一个依赖关系图。

2.  **拓扑排序 (Topological Sort)**：
    *   使用深度优先搜索（DFS）对依赖图进行拓扑排序。
    *   在此过程中同时检测是否存在环（Cycle）。如果发现 A->B->A 的依赖链，会立即报错。

3.  **按序执行**：
    *   根据排序后的顺序依次调用构造函数。
    *   执行结果被缓存到 `values` 映射中，供后续的组件注入使用。

4.  **资源管理**：
    *   当构造函数执行成功并返回实例后，检查该实例是否实现了 `Cleanable` 接口。
    *   如果实现，将其 `Cleanup` 方法加入到清理列表。
    *   `Cleanup()` 被调用时，逆序执行列表中的方法，确保依赖层级较低的资源（如 DB）比依赖它的资源（如 Service）更晚关闭。
