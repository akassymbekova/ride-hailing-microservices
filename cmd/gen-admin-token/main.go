package main

import (
	"fmt"
	"os"
	"time"

	"ride-hail/internal/shared/auth"
)

func main() {
	subject := "33333333-3333-4333-8333-333333333333"
	if v := os.Getenv("SUBJECT"); v != "" {
		subject = v
	}
	role := "ADMIN"
	if v := os.Getenv("ROLE"); v != "" {
		role = v
	}

	token, err := auth.GenerateToken(subject, role, 24*time.Hour)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(token)
}
