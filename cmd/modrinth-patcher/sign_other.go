//go:build !darwin

package main

func codesignAdHoc(app string) error { return nil }
