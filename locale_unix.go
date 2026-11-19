//go:build !windows && !darwin

package main

func nativeLocale() string { return "" }
