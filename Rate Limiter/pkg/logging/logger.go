package logging

import "fmt"

type Logger interface {
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

type NoOpLogger struct{}

func (l *NoOpLogger) Info(msg string, fields map[string]interface{}) {}

func (l *NoOpLogger) Warn(msg string, fields map[string]interface{})  {}

func (l *NoOpLogger) Error(msg string, fields map[string]interface{}) {}

type SimpleLogger struct{}

func (l *SimpleLogger) Info(msg string, fields map[string]interface{}) {
	printLevel("INFO", msg, fields)
}

func (l *SimpleLogger) Warn(msg string, fields map[string]interface{}) {
	printLevel("WARN", msg, fields)
}

func (l *SimpleLogger) Error(msg string, fields map[string]interface{}) {
	printLevel("ERROR", msg, fields)
}

func printLevel(level, msg string, fields map[string]interface{}) {
	fmt.Printf("[%s] %s", level, msg)
	if len(fields) > 0 {
		fmt.Print(" ")
		first := true
		for k, v := range fields {
			if !first {
				fmt.Print(", ")
			}
			fmt.Printf("%s=%v", k, v)
			first = false
		}
	}
	fmt.Println()
}
