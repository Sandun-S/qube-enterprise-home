# 02 - Go Crash Course for Python Developers

You mentioned you are familiar with Python but new to Go (Golang). Here is a cheat sheet to translate Python concepts to Go, especially tailored for this codebase.

## 1. Project Setup: `go.mod` and `go.sum`
In Python, you have `requirements.txt` or `Pipfile` to track installed libraries.
In Go, you use **Modules**:
- `go.mod`: Like `Pipfile` or `pyproject.toml`. It defines your project module name and lists the direct dependencies (e.g., PostgreSQL drivers, web frameworks).
- `go.sum`: Like `Pipfile.lock`. It contains cryptographic hashes of all downloaded packages to ensure the build exactly matches on every machine. You never edit this by hand.

## 2. The Entry Point: `main.go`
In Python, execution usually starts where `if __name__ == "__main__":` is placed.
In Go, execution **always** starts in a package named `main`, specifically inside a function called `func main()`.
Whenever you look at a Go service (like `conf-agent`), look for `main.go` and `func main()` to see where execution begins.

## 3. Running and Building
- **Python:** `python main.py` (Script is interpreted at runtime)
- **Go:** `go run main.go` (Compiles on the fly and runs)
- **Go Build:** `go build -o myapp` (Creates a standalone compiled binary file `myapp` that runs natively on the OS without needing Go installed!). This is why Go is used for edge devices—we just ship the tiny compiled binary.

## 4. Static Typing vs Dynamic Typing
- **Python:** `x = 10`
- **Go:** `var x int = 10` or more commonly `x := 10` (Go infers the type, but the type is permanently locked).

You'll see a lot of `err` handling in Go. In Python, you use `try...except`. In Go, functions return multiple values, often bringing back the result *and* an error object:
```go
// Go style
result, err := DoSomething()
if err != nil {
    // handle error
    log.Fatal("Failed!")
}
```

## 5. Structs instead of Classes
Go does not have "Classes" or inheritance like Python. It uses **Structs** (data containers) and **Methods** (functions attached to structs).
```go
// Similar to a Python Class definition
type Device struct {
    ID   string
    Name string
}

// Similar to a Python Class method
func (d *Device) TurnOn() {
    fmt.Println(d.Name, "is on")
}
```

## 6. Concurrency: Goroutines vs Python's `asyncio`
In Python, running things concurrently is complex (`threading` or `asyncio`).
In Go, it is built-in and incredibly easy using **Goroutines**.
Just put the keyword `go` in front of a function call, and it runs in the background. Look at `cloud/cmd/server/main.go`; you'll see:
```go
go func() {
    // Run the TP-API server on port 8081 in the background
    http.ListenAndServe(":8081", ...)
}()

// Run the Cloud API on port 8080 in the main thread
http.ListenAndServe(":8080", ...)
```

## 7. Pointers `*` and `&`
You'll see symbols like `&` and `*` in the code.
- `&x` means "get the memory address of variable x" (passing by reference so it can be modified).
- `*x` means "read the value at this memory address."
This prevents Go from making heavy copies of large data objects in memory.
