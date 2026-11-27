//go:build !windows

package main

func canInterruptChild() bool { return true }
