package logger

import (
	"log"
	"os"
)

var debugMode = false

func init() {
	if os.Getenv("DEBUG") == "true" {
		debugMode = true
	}
}

// IsDebug returns true if debug mode is active.
func IsDebug() bool {
	return debugMode
}

// Debugf prints the message only when DEBUG=true.
func Debugf(format string, v ...any) {
	if debugMode {
		log.Printf(format, v...)
	}
}

// Debugln prints the message only when DEBUG=true.
func Debugln(v ...any) {
	if debugMode {
		log.Println(v...)
	}
}

// Printf logs a message to stdout/stderr.
func Printf(format string, v ...any) {
	log.Printf(format, v...)
}

// Println logs a message to stdout/stderr.
func Println(v ...any) {
	log.Println(v...)
}

// Fatalf logs a message and exits with status 1.
func Fatalf(format string, v ...any) {
	log.Fatalf(format, v...)
}

// Fatalln logs a message and exits with status 1.
func Fatalln(v ...any) {
	log.Fatalln(v...)
}

// Fatal logs a message and exits with status 1.
func Fatal(v ...any) {
	log.Fatal(v...)
}
