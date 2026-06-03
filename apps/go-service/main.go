package main

import "fmt"

func main() {
	fmt.Println(Greet("Bazel"))
}

func Greet(name string) string {
	return fmt.Sprintf("Hello from Go, %s!", name)
}
