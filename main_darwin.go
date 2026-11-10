package main

// runHelper reports whether the executable was started for something other
// than the program; on macOS it never is.
func runHelper() bool { return false }
