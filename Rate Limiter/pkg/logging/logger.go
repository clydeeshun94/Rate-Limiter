package logging // logging: structured logging interface for rate limiter

import "fmt" // fmt: formatting for log output

// Logger is the interface for all logging implementations. // Logger: standard logging contract
type Logger interface { // Logger: three severity levels with structured fields
	Info(msg string, fields map[string]interface{}) // Info: informational message with optional key-value fields
	Warn(msg string, fields map[string]interface{}) // Warn: warning message with optional key-value fields
	Error(msg string, fields map[string]interface{}) // Error: error message with optional key-value fields
}

// NoOpLogger is a Logger that does nothing (discards all log output). // NoOpLogger: null logger for production silence
type NoOpLogger struct{} // NoOpLogger: empty struct (no fields)

// Info implements Logger (no-op). // Info: discards message and fields
func (l *NoOpLogger) Info(msg string, fields map[string]interface{}) {} // no operation

// Warn implements Logger (no-op). // Warn: discards message and fields
func (l *NoOpLogger) Warn(msg string, fields map[string]interface{}) {} // no operation

// Error implements Logger (no-op). // Error: discards message and fields
// Note: even Error is a no-op — callers must configure a real logger for production.
func (l *NoOpLogger) Error(msg string, fields map[string]interface{}) {} // no operation

// SimpleLogger is a Logger that prints to stdout with severity prefix. // SimpleLogger: basic console logger
type SimpleLogger struct{} // SimpleLogger: empty struct (no fields, stateless)

// Info logs an informational message to stdout. // Info: prints [INFO] prefix with message and fields
func (l *SimpleLogger) Info(msg string, fields map[string]interface{}) { // Info: delegates to printLevel
	printLevel("INFO", msg, fields) // printLevel: handles formatting and output
}

// Warn logs a warning message to stdout. // Warn: prints [WARN] prefix with message and fields
func (l *SimpleLogger) Warn(msg string, fields map[string]interface{}) { // Warn: delegates to printLevel
	printLevel("WARN", msg, fields) // printLevel: handles formatting and output
}

// Error logs an error message to stdout. // Error: prints [ERROR] prefix with message and fields
func (l *SimpleLogger) Error(msg string, fields map[string]interface{}) { // Error: delegates to printLevel
	printLevel("ERROR", msg, fields) // printLevel: handles formatting and output
}

// printLevel formats and prints a log message with severity prefix and structured fields. // printLevel: shared formatter for all log levels
func printLevel(level, msg string, fields map[string]interface{}) { // printLevel: writes to stdout (not thread-safe)
	fmt.Printf("[%s] %s", level, msg) // print severity bracket and message (no newline yet)
	if len(fields) > 0 { // if there are structured fields
		fmt.Print(" ") // space separator before fields
		first := true // first: flag to avoid leading comma
		for k, v := range fields { // k: field key; v: field value
			if !first { // if not the first field
				fmt.Print(", ") // comma-space separator between fields
			}
			fmt.Printf("%s=%v", k, v) // print key=value pair
			first = false // subsequent fields are not first
		}
	}
	fmt.Println() // newline at end of log entry
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. INTERFACE-BASED LOGGING
   - Logger is an interface, not a concrete implementation.
   - Decision: allows swapping NoOpLogger (production) with SimpleLogger (development) or external loggers (structured JSON, etc.).
   - Adjuster config has a Logger field with NoOpLogger default — zero-cost if unused.

2. NO-OP DEFAULT
   - NoOpLogger is the default when config.Logger is nil.
   - Decision: avoids forcing callers to provide a logger. Logging is optional.
   - This prevents "logging required" errors during setup while still allowing it.

3. STRUCTURED FIELDS (map[string]interface{})
   - Each log call can include key-value pairs (e.g., {"identity": "alice", "limit": 100}).
   - Decision: structured logging enables programmatic log parsing.
   - Alternative: key-value string formatting. Maps are more type-safe.

4. MAP ITERATION ORDER IS NON-DETERMINISTIC
   - Go maps iterate in random order. Field order in output varies between calls.
   - Decision: acceptable for log output (order doesn't matter for parsing).
   - If deterministic order needed, use sorted keys (adds complexity).

5. PRINTF TO STDOUT
   - SimpleLogger uses fmt.Printf directly.
   - Decision: simplest implementation. No external dependencies.
   - Production alternative: write to files, syslog, or external services (Datadog, etc.).

6. THREE LEVELS ONLY (Info, Warn, Error)
   - No Debug or Trace levels.
   - Decision: rate limiter has low volume; 3 levels are sufficient.
   - Info = normal operations, Warn = rate limit hits/adjustments, Error = failures.

7. THREAD SAFETY
   - SimpleLogger is stateless but printLevel uses fmt.Printf which is thread-safe in Go.
   - NoOpLogger is fully thread-safe (no state).
   - Decision: safe for concurrent use by multiple rate limiter algorithms.

8. FORMAT: [LEVEL] MESSAGE key1=val1, key2=val2
   - Single-line format with severity bracket and space-separated key=value pairs.
   - Decision: easy to grep, parse, and read. Common log format convention.
*/
